package storekit

// images.go — THE CHARLY IMAGE STORE: the engine-neutral artefact of every build.
//
// One OCI image layout at `<data root>/images` — `oci-layout` + ONE `index.json` +
// `blobs/<alg>/<digest>` — holding every image this host has built. It is engine-NEUTRAL on
// purpose: the build's canonical output must not belong to whichever container engine happens to
// be selected, because the next deploy may resolve a different one.
//
// ⚑ THE NAMING ANNOTATION IS THE WHOLE CONTRACT, and it is `io.containerd.image.name`, NOT
// `org.opencontainers.image.ref.name`. Measured on buildkit 0.34.1: a `--output
// type=oci,name=<full-ref>` export writes BOTH — `io.containerd.image.name` = `<repo>:<tag>` and
// `org.opencontainers.image.ref.name` = the TAG ALONE. Every engine measured (podman 6.1.4,
// docker 29.9.2, nerdctl 2.4.1) imports such an archive with the full reference already applied
// and needs NO `tag` step; an archive built WITHOUT `name=` lands untagged/dangling everywhere.
// Store lookups therefore key on `io.containerd.image.name` only — keying on the tag-only
// annotation would match the wrong image (or none) for every namespaced ref.
//
// # What is shared, and what is this file's own
//
// The LAYOUT is `Store`'s and is not re-implemented here: the blob directory, the
// content-addressed paths, the atomic write, the cross-process index lock and the `oci-layout`
// marker all come from store.go/entry.go. This file adds only what an IMAGE store needs on top —
// the reference vocabulary that keys it, and the two archive forms that cross the engine
// boundary (an export coming in, a loadable archive going out).
//
// `Store` is deliberately NOT embedded. Its `Entries`/`PutEntry` vocabulary is the CACHE store's,
// and an image store that could be mistaken for one would misparse every descriptor it holds.
//
// # The root, and the one relocation
//
// The store lives under the DATA root, never the cache root: a built image is a durable artefact
// whose loss costs a rebuild, while the cache root is disposable by contract (a `cache clear`
// must be able to delete it) — so `CHARLY_CACHE_DIR`, which relocates the cache root, must not
// carry the built images with it. `CHARLY_IMAGE_STORE_DIR` is this store's own relocation, and a
// check bed MUST use it: a bed's build must not write the operator's store.

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/opencharly/spec/lock"
)

const (
	// ImageStoreName is the name of the ONE image store a host builds into — the `charly` store.
	// A solve addresses it as `oci-layout://charly@<manifest-digest>`. It is a NAME, not a path
	// element: the layout itself lives at `<data root>/images`.
	ImageStoreName = "charly"
	// ImageStoreDirEnv relocates the store for a disposable target, exactly as `CHARLY_CACHE_DIR`
	// relocates the cache root. A check bed MUST use it: a bed's build must not write the
	// operator's store.
	ImageStoreDirEnv = "CHARLY_IMAGE_STORE_DIR"
	// imageStoreDirName is the layout's path element under the charly data root.
	imageStoreDirName = "images"

	// AnnotationImageName is the annotation that NAMES an image inside an OCI layout index: the
	// full `<repo>:<tag>` reference, which is what an engine applies on load and what every store
	// lookup keys on. See the header for why it is not AnnotationRefName.
	AnnotationImageName = "io.containerd.image.name"

	// The OCI image media types an archive's blob graph is walked by. The two INDEX types name a
	// set of manifests; the two MANIFEST types name a config plus layers. Anything else is an
	// opaque leaf and is copied by digest without being interpreted.
	mediaTypeOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	mediaTypeDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// archiveEpoch is the modification time every member of a materialised archive carries. A fixed
// time is what makes two archives of one stored image the same bytes: the store's blobs are
// immutable and its index is a pure function of them, so the timestamps are the only thing that
// could differ per call, and they must not.
var archiveEpoch = time.Unix(0, 0)

// ImageStoreDir resolves the charly image store's layout directory: `CHARLY_IMAGE_STORE_DIR` when
// set, otherwise `images/` under the DATA root.
//
// It resolves through `DataDir()` — the contract module's ONE data-root accessor, re-exported by
// this package — rather than composing `$XDG_DATA_HOME` here. A root resolved in a second place is
// the deviation storekit exists to end: the host and every plugin must get the same answer from
// the same code, or a plugin writes an image the host can never find.
func ImageStoreDir() (string, error) {
	if dir := os.Getenv(ImageStoreDirEnv); dir != "" {
		return dir, nil
	}
	root, err := DataDir()
	if err != nil {
		return "", fmt.Errorf("resolve the %s image store: %w", ImageStoreName, err)
	}
	return filepath.Join(root, imageStoreDirName), nil
}

// ImageStore is the charly image store: ONE OCI image layout under the DATA root, keyed by the
// reference each stored manifest was built as.
type ImageStore struct {
	store *Store
}

// OpenImageStore opens the charly image store at its resolved directory.
func OpenImageStore() (*ImageStore, error) {
	dir, err := ImageStoreDir()
	if err != nil {
		return nil, err
	}
	return OpenImageStoreDir(dir), nil
}

// OpenImageStoreDir opens the image store rooted at dir — a disposable target's relocated store,
// or a test's own tree. It accepts a directory that does not exist yet: the store is created by
// the first `Add`, exactly as a cache store is created by its first `PutBlob`.
func OpenImageStoreDir(dir string) *ImageStore { return &ImageStore{store: OpenStoreDir(dir)} }

// Dir is the layout directory.
func (s *ImageStore) Dir() string { return s.store.dir }

// namesRef reports whether a descriptor is keyed by ref. THE KEY IS `io.containerd.image.name`:
// the tag-only annotation cannot tell two references of one repository apart.
func namesRef(d indexDescriptor, ref string) bool {
	return d.Annotations[AnnotationImageName] == ref
}

// lookupAll returns every descriptor the index names for ref, in index order. A multi-platform
// build writes ONE descriptor per platform and every one carries the same reference, so this is a
// set, never a single answer.
func (s *ImageStore) lookupAll(ref string) ([]indexDescriptor, error) {
	idx, err := s.store.readIndex()
	if err != nil {
		return nil, err
	}
	var out []indexDescriptor
	for _, d := range idx.Manifests {
		if namesRef(d, ref) {
			out = append(out, d)
		}
	}
	return out, nil
}

// Lookup returns the MANIFEST digest ref resolves to inside the store.
func (s *ImageStore) Lookup(ref string) (digest string, ok bool, err error) {
	descs, err := s.lookupAll(ref)
	if err != nil {
		return "", false, err
	}
	if len(descs) == 0 {
		return "", false, nil
	}
	return descs[0].Digest, true, nil
}

// List returns every reference the store holds, deduplicated and sorted — one reference per
// image, not one per platform, because that is what a caller elects a tag over.
func (s *ImageStore) List() ([]string, error) {
	idx, err := s.store.readIndex()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(idx.Manifests))
	refs := make([]string, 0, len(idx.Manifests))
	for _, d := range idx.Manifests {
		ref := d.Annotations[AnnotationImageName]
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs, nil
}

// Add copies an exported OCI archive's blobs into the store and merges its manifest
// descriptor(s) into the store index, replacing any entry that already names ref. One descriptor
// is written per manifest in the export (a multi-platform `--push` export contributes one per
// platform); a rebuild replaces the whole previous set for that reference.
//
// The blobs are content-addressed, so an existing blob is the same bytes and is left alone; the
// index is the only mutable file and it is rewritten atomically under the store's cross-process
// lock (two levels of a build write the store concurrently).
//
// THE ARCHIVE MUST ALREADY BE NAMED, and named with THIS reference. Both refusals are about the
// same property from two sides: an entry that is not keyed by a reference is unreachable, and an
// entry filed under a reference it does not name would be found by a lookup and materialised as
// the wrong image. The same archive is what an engine load later reads, so a caller leaning on
// the store to supply the name would still be feeding an unaddressable archive to that load — the
// name belongs in the export, and this call checks it rather than inventing it.
func (s *ImageStore) Add(archivePath, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("add an image to the %s image store: no reference to key it by", ImageStoreName)
	}
	if err := s.store.EnsureLayout(); err != nil {
		return err
	}
	exported, err := readArchiveIndex(archivePath)
	if err != nil {
		return err
	}
	if len(exported.Manifests) == 0 {
		return fmt.Errorf("the OCI archive %s carries no image manifest", archivePath)
	}
	for i, d := range exported.Manifests {
		named := d.Annotations[AnnotationImageName]
		if named == "" {
			return fmt.Errorf("the OCI archive %s carries no %s annotation on manifest %d: an archive without it lands unaddressable", archivePath, AnnotationImageName, i)
		}
		if named != ref {
			return fmt.Errorf("the OCI archive %s names %q but is being added as %q: the store's key IS the reference the image was built as", archivePath, named, ref)
		}
	}
	if err := s.copyArchiveBlobs(archivePath); err != nil {
		return err
	}

	lockPath, err := s.store.indexLockPath()
	if err != nil {
		return err
	}
	release, err := lock.AcquireFileLockWithin(lockPath, true, indexLockWait)
	if err != nil {
		return fmt.Errorf("lock the %s image store index: %w", ImageStoreName, err)
	}
	defer func() { _ = release() }() // the release error carries nothing a caller can act on

	idx, err := s.store.readIndex()
	if err != nil {
		return err
	}
	// A fresh slice, never an in-place `[:0]` re-slice: the aliasing form appends into the OLD
	// backing array while assigning to a different name, which reads like a copy and is not one.
	manifests := make([]indexDescriptor, 0, len(idx.Manifests)+len(exported.Manifests))
	for _, d := range idx.Manifests {
		if namesRef(d, ref) {
			continue // superseded by this build
		}
		manifests = append(manifests, d)
	}
	manifests = append(manifests, exported.Manifests...)
	idx.Manifests = manifests
	raw, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("render the %s image store index: %w", ImageStoreName, err)
	}
	if err := writeFileAtomic(s.store.indexPath(), raw); err != nil {
		return fmt.Errorf("write the %s image store index: %w", ImageStoreName, err)
	}
	return nil
}

// Archive materialises ref as an OCI image archive in a NEW temporary file and returns its path.
// The file belongs to the CALLER, who removes it: an archive is as large as the image it carries,
// and only the consumer at the engine boundary knows when that load is done with it.
func (s *ImageStore) Archive(ref string) (string, error) {
	f, err := os.CreateTemp("", "charly-image-*.oci")
	if err != nil {
		return "", fmt.Errorf("stage an archive of %s: %w", ref, err)
	}
	path := f.Name()
	if err := s.writeArchive(ref, f); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("finish the OCI archive %s: %w", path, err)
	}
	return path, nil
}

// WriteArchive writes ref as an OCI image archive to w without staging a file — the same archive
// `Archive` materialises, for a caller that streams it straight at an engine.
func (s *ImageStore) WriteArchive(ref string, w io.Writer) error { return s.writeArchive(ref, w) }

// writeArchive is the ONE archive writer: an `oci-layout` marker, an `index.json` carrying the
// stored descriptors (and therefore the naming annotation), and every blob the image needs.
func (s *ImageStore) writeArchive(ref string, w io.Writer) error {
	descs, err := s.lookupAll(ref)
	if err != nil {
		return err
	}
	if len(descs) == 0 {
		return fmt.Errorf("the %s image store %s holds no image %q", ImageStoreName, s.store.dir, ref)
	}
	blobs, err := s.archiveBlobs(descs)
	if err != nil {
		return err
	}
	indexJSON, err := json.Marshal(&storeIndex{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIIndex,
		Manifests:     descs,
	})
	if err != nil {
		return fmt.Errorf("render the index for %s: %w", ref, err)
	}

	tw := tar.NewWriter(w)
	if err := tarFile(tw, LayoutFile, bytes.NewReader([]byte(layoutMarker)), int64(len(layoutMarker))); err != nil {
		return fmt.Errorf("write the %s marker into the archive of %s: %w", LayoutFile, ref, err)
	}
	if err := tarFile(tw, indexFile, bytes.NewReader(indexJSON), int64(len(indexJSON))); err != nil {
		return fmt.Errorf("write the %s of %s: %w", indexFile, ref, err)
	}
	for _, dgst := range blobs {
		name, err := s.blobMemberName(dgst)
		if err != nil {
			return err
		}
		path, err := s.store.blobPath(dgst)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open blob %s for %s: %w", dgst, ref, err)
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close() // read-only open; the close error carries nothing
			return fmt.Errorf("size blob %s for %s: %w", dgst, ref, err)
		}
		werr := tarFile(tw, name, f, info.Size())
		_ = f.Close() // read-only open; the close error carries nothing
		if werr != nil {
			return fmt.Errorf("write blob %s into the archive of %s: %w", dgst, ref, werr)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("finish the archive of %s: %w", ref, err)
	}
	return nil
}

// blobMemberName renders a digest's archive member name (`blobs/<alg>/<hex>`).
func (s *ImageStore) blobMemberName(dgst string) (string, error) {
	if _, err := s.store.blobPath(dgst); err != nil {
		return "", err
	}
	alg, hexPart, _ := strings.Cut(dgst, ":")
	return blobDir + "/" + alg + "/" + hexPart, nil
}

// tarFile appends one regular file to an archive, streaming size bytes from src.
//
// An entry name is data read by an engine, so it is written exactly as the layout spells it; the
// mode and the timestamp are fixed, because an archive of immutable content-addressed bytes must
// be the same bytes every time it is materialised.
func tarFile(tw *tar.Writer, name string, src io.Reader, size int64) error {
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     0o644,
		Size:     size,
		ModTime:  archiveEpoch,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	// tar.Writer refuses a stream that writes more or fewer than Size bytes, so a blob that
	// changed under us fails loudly here instead of producing a truncated image.
	_, err := io.Copy(tw, src)
	return err
}

// archiveBlobs returns every blob digest the descriptors reach — the manifests themselves, their
// configs, and their layers — in a stable order.
//
// It walks the GRAPH rather than copying `blobs/`: an image store holds every image it ever built,
// so an archive made by copying the whole blob directory would carry every unrelated image too.
// Only the four structural media types are interpreted; a config or a layer is an opaque leaf that
// must merely EXIST, which is also what keeps a multi-gigabyte layer out of memory.
func (s *ImageStore) archiveBlobs(descs []indexDescriptor) ([]string, error) {
	seen := make(map[string]bool, len(descs))
	var order []string
	var walk func(d indexDescriptor) error
	walk = func(d indexDescriptor) error {
		if d.Digest == "" {
			return fmt.Errorf("the %s image store carries a descriptor with no digest (media type %q)", ImageStoreName, d.MediaType)
		}
		if seen[d.Digest] {
			return nil
		}
		seen[d.Digest] = true
		order = append(order, d.Digest)
		switch d.MediaType {
		case MediaTypeOCIIndex, mediaTypeDockerList:
			raw, err := s.store.ReadBlob(d.Digest)
			if err != nil {
				return err
			}
			var nested storeIndex
			if err := json.Unmarshal(raw, &nested); err != nil {
				return fmt.Errorf("parse the image index %s: %w", d.Digest, err)
			}
			for _, child := range nested.Manifests {
				if err := walk(child); err != nil {
					return err
				}
			}
		case mediaTypeOCIManifest, mediaTypeDockerManifest:
			raw, err := s.store.ReadBlob(d.Digest)
			if err != nil {
				return err
			}
			var manifest struct {
				Config indexDescriptor   `json:"config"`
				Layers []indexDescriptor `json:"layers"`
			}
			if err := json.Unmarshal(raw, &manifest); err != nil {
				return fmt.Errorf("parse the image manifest %s: %w", d.Digest, err)
			}
			if err := walk(manifest.Config); err != nil {
				return err
			}
			for _, layer := range manifest.Layers {
				if err := walk(layer); err != nil {
					return err
				}
			}
		default:
			// An opaque leaf (a config, a layer): its bytes are copied by digest and never
			// interpreted, but a missing one would silently truncate the image.
			if !s.store.HasBlob(d.Digest) {
				return fmt.Errorf("the %s image store names blob %s (%q), which it does not hold", ImageStoreName, d.Digest, d.MediaType)
			}
		}
		return nil
	}
	for _, d := range descs {
		switch d.MediaType {
		case MediaTypeOCIIndex, mediaTypeDockerList, mediaTypeOCIManifest, mediaTypeDockerManifest:
		default:
			// The TOP level is strict: a descriptor this store names as an image and cannot
			// interpret would be archived as a bare blob with its contents dropped.
			return nil, fmt.Errorf("cannot archive %s: unknown image media type %q", d.Digest, d.MediaType)
		}
		if err := walk(d); err != nil {
			return nil, err
		}
	}
	sort.Strings(order)
	return order, nil
}

// readArchiveIndex reads an OCI archive's `index.json` without rewriting the archive.
func readArchiveIndex(archivePath string) (*storeIndex, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open the OCI archive %s: %w", archivePath, err)
	}
	defer func() { _ = f.Close() }() // read-only open; the close error carries nothing

	tr := tar.NewReader(f)
	for {
		hdr, rerr := tr.Next()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("read the OCI archive %s: %w", archivePath, rerr)
		}
		if strings.TrimPrefix(hdr.Name, "./") != indexFile {
			continue
		}
		raw, rerr := io.ReadAll(tr)
		if rerr != nil {
			return nil, fmt.Errorf("read %s from %s: %w", indexFile, archivePath, rerr)
		}
		var idx storeIndex
		if uerr := json.Unmarshal(raw, &idx); uerr != nil {
			return nil, fmt.Errorf("parse %s from %s: %w", indexFile, archivePath, uerr)
		}
		return &idx, nil
	}
	return nil, fmt.Errorf("the OCI archive %s carries no %s", archivePath, indexFile)
}

// copyArchiveBlobs copies every `blobs/` member of an archive into the layout, SKIPPING a blob the
// store already holds (content-addressed bytes are the same bytes, so a rebuild of a shared base
// costs nothing) and streaming each one that is new.
func (s *ImageStore) copyArchiveBlobs(archivePath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open the OCI archive %s: %w", archivePath, err)
	}
	defer func() { _ = f.Close() }() // read-only open; the close error carries nothing

	tr := tar.NewReader(f)
	for {
		hdr, rerr := tr.Next()
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("read the OCI archive %s: %w", archivePath, rerr)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if !strings.HasPrefix(name, blobDir+"/") {
			continue
		}
		dest, perr := s.archiveBlobPath(name)
		if perr != nil {
			return fmt.Errorf("the OCI archive %s carries an unusable blob path: %w", archivePath, perr)
		}
		if _, serr := os.Stat(dest); serr == nil {
			continue
		} else if !errors.Is(serr, os.ErrNotExist) {
			return serr
		}
		if merr := os.MkdirAll(filepath.Dir(dest), 0o755); merr != nil {
			return merr
		}
		// STREAM the blob: a layer can be gigabytes, and the store must not hold one in memory.
		// Durable, because these bytes are the store's only copy of a built image.
		if werr := copyFileAtomic(dest, tr, true); werr != nil {
			return fmt.Errorf("write blob %s: %w", name, werr)
		}
	}
}

// archiveBlobPath maps an archive member name (`blobs/<alg>/<hex>`) onto the store's own blob
// path. It goes through the ONE digest path builder, so a crafted archive cannot name a path that
// escapes `blobs/` — the member name is data, and the store never trusts it as a path.
func (s *ImageStore) archiveBlobPath(name string) (string, error) {
	rest, ok := strings.CutPrefix(name, blobDir+"/")
	if !ok {
		return "", fmt.Errorf("%q is not inside %s/", name, blobDir)
	}
	alg, hexPart, ok := strings.Cut(rest, "/")
	if !ok || alg == "" || hexPart == "" {
		return "", fmt.Errorf("%q is not a content-addressed blob path", name)
	}
	return s.store.blobPath(alg + ":" + hexPart)
}
