package storekit

// images.go — THE CHARLY IMAGE STORE: the engine-neutral artefact of every build.
//
// The store is the `registry` NAMED STORE under the ONE cache root — `<cache root>/registry`,
// addressed through `StoreDir` — and every decision below is a contract row:
//
//   - D-STORE-1: one root, one override. Named stores live under the cache root, so a disposable
//     target isolates with `CHARLY_CACHE_DIR` and this store follows it. There is NO second
//     override for an image store: a bed that relocated only images would be a second root
//     resolver wearing an environment variable.
//   - D-STORE-3: one content-addressed store holds every cached item INCLUDING artifacts, and "a
//     store tree is that layout and nothing else". This is a named store like any other, not a
//     second layout in a second root; its entries are immutable blobs behind an atomically
//     renamed `index.json`, and its only locks are the store's own (in the runtime root).
//   - D-STORE-10: the store is served read-only as an image registry, "a view of store ENTRIES,
//     never a second copy" — so a built image must BE an entry of that store, or the view can
//     never serve it. That is why the name is `registry`: it is the store the registry surface
//     reads, and plugin-clean's retirement record already names `<cache root>/registry` as where
//     image content lives (`<cache root>/images` is the RETIRED pre-rename blob store, so that
//     name is taken and must not be reused).
//
// ⚑ THE ENTRY MODEL IS THE STORE'S, AND IT IS THE KEYING ANSWER. Each artifact is one
// `Entry`:
//
//	Key       = the reference the caller asked for   (the INPUT identity)
//	Payload   = the artifact's own digest            (the RESOLUTION: what that name IS, as content)
//	MediaType = what that payload is
//
// so nothing immutable is ever mutated: the payload is a content-addressed blob, the entry
// manifest is a content-addressed blob, and a rebuild REPLACES the index POINTER for that key —
// exactly what `PutEntry` documents and what D-STORE-5's "re-resolves only on --refresh" means.
// The previous artifact stays addressable by digest until D-STORE-7's sweep reclaims it, which is
// what makes retention possible at all: one current entry per reference, the rest unreferenced.
//
// D-STORE-5 answers itself in that shape: a mutable name never becomes an ARTIFACT's name. It is
// the index slot a caller asks by; the artifact is addressed, stored and served by the digest the
// name resolved to at write time. This is the same shape opencharly/plugin-cache's `registry`
// ingest already writes (`Key: ref, Payload: root digest`), so a built image and an ingested one
// are the same kind of entry in the same store — the two callers the move exists for.
//
// ⚑ THE NAMING ANNOTATION IS THE EXPORT CONTRACT, and it is `io.containerd.image.name`, NOT
// `org.opencontainers.image.ref.name`. Measured on buildkit 0.34.1: a `--output
// type=oci,name=<full-ref>` export writes BOTH — `io.containerd.image.name` = `<repo>:<tag>` and
// `org.opencontainers.image.ref.name` = the TAG ALONE. Every engine measured (podman 6.1.4,
// docker 29.9.2, nerdctl 2.4.1) imports such an archive with the full reference already applied
// and needs NO `tag` step; an archive built WITHOUT `name=` lands untagged/dangling everywhere.
// It is an EXPORT/IMPORT annotation, not a store key: the store's key is the entry above, and the
// payload therefore carries no name and no timestamp (D-STORE-6) — the archive materialised for an
// engine re-applies it.

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
)

const (
	// ImageStoreName is the name of the ONE store a host's built images live in: the `registry`
	// named store under the cache root. It is a NAME, not a path element under a data root — see
	// the header for why this name and not `images` (retired) or a second root (D-STORE-1).
	ImageStoreName = "registry"

	// AnnotationImageName is the annotation that NAMES an image inside an OCI layout index, in an
	// ARCHIVE crossing the engine boundary. See the header for why it is not AnnotationRefName.
	AnnotationImageName = "io.containerd.image.name"

	// The OCI image media types the artifact graph is walked by, and the ones an entry's media
	// type may declare. The two INDEX types name a set of manifests; the two MANIFEST types name
	// a config plus layers. Anything else is an opaque leaf.
	mediaTypeOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	mediaTypeDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// archiveEpoch is the modification time every member of a materialised archive carries. A fixed
// time is what makes two archives of one stored image the same bytes: the store's blobs are
// immutable, so the timestamps are the only thing that could differ per call.
var archiveEpoch = time.Unix(0, 0)

// ImageStoreDir resolves the image store's layout directory: the `registry` named store under the
// cache root, through the ONE resolver (`StoreDir`), so `CHARLY_CACHE_DIR` relocates it with every
// other store. The build drive needs the DIR (a solve takes the layout as `--oci-layout
// <id>=<dir>`); it must never compose one, which is why there is no constructor taking a dir.
func ImageStoreDir() (string, error) { return StoreDir(ImageStoreName) }

// ImageStore is the charly image store: the named `registry` store, whose entries are artifacts
// keyed by the reference each build was asked for and addressed by the digest it resolved to.
type ImageStore struct {
	store *Store
}

// OpenImageStore opens the charly image store at its resolved directory.
func OpenImageStore() (*ImageStore, error) {
	store, err := OpenStore(ImageStoreName)
	if err != nil {
		return nil, err
	}
	return &ImageStore{store: store}, nil
}

// Dir is the layout directory.
func (s *ImageStore) Dir() string { return s.store.dir }

// isImageMediaType reports whether a media type names an OCI image manifest or index — the four
// structural types whose bytes this package may interpret. Anything else (a config, a layer, a
// cache payload) is an opaque leaf it must never parse.
func isImageMediaType(mediaType string) bool {
	switch mediaType {
	case MediaTypeOCIIndex, mediaTypeOCIManifest, mediaTypeDockerList, mediaTypeDockerManifest:
		return true
	default:
		return false
	}
}

// Lookup returns the digest ref resolves to inside the store — the entry's payload, which is the
// artifact's own content address.
func (s *ImageStore) Lookup(ref string) (digest string, ok bool, err error) {
	entry, found, err := s.store.LookupEntry(ref)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, nil
	}
	return entry.Payload, true, nil
}

// Has reports whether the store holds ref.
func (s *ImageStore) Has(ref string) (bool, error) {
	_, ok, err := s.store.LookupEntry(ref)
	return ok, err
}

// List returns every reference the store holds, sorted — the entries' keys, which is the store's
// own inventory (D-STORE-7's "enumerate entries", satisfied by the model rather than by a
// listing sidecar).
func (s *ImageStore) List() ([]string, error) {
	entries, err := s.store.Entries()
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0, len(entries))
	for _, e := range entries {
		refs = append(refs, e.Key)
	}
	sort.Strings(refs)
	return refs, nil
}

// Add lands an exported OCI archive as ONE store entry keyed by ref.
//
// The order is the contract: blobs first, the entry LAST, so a reader never observes a key whose
// payload is not yet published. Existing blobs are not rewritten (content-addressed bytes are the
// same bytes), and the entry's payload is the archive's own `index.json` — the OCI layout's entry
// point — so a single-platform and a multi-platform export are stored the same way and
// `Archive` reproduces either.
//
// The archive must ALREADY name ref with the export annotation, because the same file is what an
// engine load reads; a caller that leaned on the store to supply the name would still be feeding
// an unaddressable archive to that load. Its copy of that annotation is STRIPPED from the payload
// (D-STORE-6: a stored payload carries no name and no timestamp) and re-applied by `Archive`,
// which is the only surface that crosses the engine boundary.
func (s *ImageStore) Add(archivePath, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("add an image to the %s store: no reference to key it by", ImageStoreName)
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
			return fmt.Errorf("the OCI archive %s names %q but is being added as %q: the entry's key IS the reference the image was built as", archivePath, named, ref)
		}
	}
	if err := s.copyArchiveBlobs(archivePath); err != nil {
		return err
	}

	// The PAYLOAD is the layout's entry point: the archive's `index.json`, stripped of the naming
	// and creation annotations the export carries for the engine. Its digest is the artifact's
	// content address, which is what the entry records as the resolution of ref.
	payload, err := json.Marshal(&storeIndex{
		SchemaVersion: 2,
		MediaType:     indexMediaType(exported),
		Manifests:     stripNamingAnnotations(exported.Manifests),
	})
	if err != nil {
		return fmt.Errorf("render the artifact index of %s: %w", ref, err)
	}
	payloadDigest, err := s.store.PutBlob(payload)
	if err != nil {
		return fmt.Errorf("publish the artifact index of %s: %w", ref, err)
	}
	if _, err := s.store.PutEntry(Entry{
		Key:       ref,
		MediaType: indexMediaType(exported),
		Payload:   payloadDigest,
	}); err != nil {
		return fmt.Errorf("index %s in the %s store: %w", ref, ImageStoreName, err)
	}
	return nil
}

// indexMediaType is the artifact's media type: what the archive's own index declares, defaulting
// to the OCI image index it is.
func indexMediaType(idx *storeIndex) string {
	if idx.MediaType != "" {
		return idx.MediaType
	}
	return MediaTypeOCIIndex
}

// stripNamingAnnotations returns the descriptors without the export's naming/creation
// annotations, so the stored payload holds no name and no timestamp (D-STORE-6) — the entry's key
// and payload already carry what a reader needs.
func stripNamingAnnotations(descs []indexDescriptor) []indexDescriptor {
	out := make([]indexDescriptor, len(descs))
	for i, d := range descs {
		annotations := make(map[string]string, len(d.Annotations))
		for k, v := range d.Annotations {
			switch k {
			case AnnotationImageName, AnnotationRefName, AnnotationCreated:
				continue
			}
			annotations[k] = v
		}
		if len(annotations) == 0 {
			annotations = nil
		}
		d.Annotations = annotations
		out[i] = d
	}
	return out
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

// WriteArchive writes ref as an OCI image archive to w without staging a file.
func (s *ImageStore) WriteArchive(ref string, w io.Writer) error { return s.writeArchive(ref, w) }

// writeArchive is the ONE archive writer: an `oci-layout` marker, an `index.json` naming the
// reference for the engine, and every blob the artifact reaches.
func (s *ImageStore) writeArchive(ref string, w io.Writer) error {
	entry, ok, err := s.store.LookupEntry(ref)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the %s store %s holds no image %q", ImageStoreName, s.store.dir, ref)
	}
	indexJSON, err := s.archiveIndex(ref, entry)
	if err != nil {
		return err
	}
	blobs, err := s.blobsOf(entry)
	if err != nil {
		return err
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

// archiveIndex re-reads the stored artifact index and re-applies the export annotation, which is
// what makes the archive addressable on load and what the payload deliberately does not carry.
func (s *ImageStore) archiveIndex(ref string, entry *Entry) ([]byte, error) {
	raw, err := s.store.ReadBlob(entry.Payload)
	if err != nil {
		return nil, fmt.Errorf("read the stored index of %s: %w", ref, err)
	}
	var idx storeIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("parse the stored index of %s: %w", ref, err)
	}
	for i := range idx.Manifests {
		if idx.Manifests[i].Annotations == nil {
			idx.Manifests[i].Annotations = map[string]string{}
		}
		idx.Manifests[i].Annotations[AnnotationImageName] = ref
	}
	idx.SchemaVersion = 2
	if idx.MediaType == "" {
		idx.MediaType = MediaTypeOCIIndex
	}
	out, err := json.Marshal(&idx)
	if err != nil {
		return nil, fmt.Errorf("render the archive index of %s: %w", ref, err)
	}
	return out, nil
}

// blobsOf returns every blob the entry's artifact reaches, sorted, excluding the entry's own
// payload when it IS the root document the archive writes as `index.json` (that member is written
// from the annotated copy, not from the stored bytes).
func (s *ImageStore) blobsOf(entry *Entry) ([]string, error) {
	raw, err := s.store.ReadBlob(entry.Payload)
	if err != nil {
		return nil, fmt.Errorf("read the stored index: %w", err)
	}
	var idx storeIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("parse the stored index: %w", err)
	}
	seen := make(map[string]bool, len(idx.Manifests))
	blobs := make([]string, 0, len(idx.Manifests))
	for _, d := range idx.Manifests {
		if err := s.store.walkImageGraph(d.Digest, d.MediaType, func(dgst string) error {
			if !seen[dgst] {
				seen[dgst] = true
				blobs = append(blobs, dgst)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sort.Strings(blobs)
	return blobs, nil
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
// store already holds (content-addressed bytes are the same bytes) and streaming each one that is
// new.
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

// walkImageGraph calls visit for every blob the image at dgst reaches — the manifest or index
// itself first, then its config, its layers, and (for an index) each child manifest and its own
// graph — once per digest.
//
// ONE WALK, TWO CALLERS, because two walks would be two answers to "what does this image need":
//
//   - `ReachableBlobs` uses it so a sweep can never free a blob a live entry reaches
//     (D-STORE-7: "charly clean keeps what state references and evicts the rest");
//   - `Archive` uses it to decide what an archive must carry.
//
// A digest's media type decides whether its bytes are INTERPRETED at all: only the four image
// media types are parsed, and anything else — a config, a layer, an arbitrary cache payload — is
// an opaque leaf that is visited and never read. That is what keeps this store's image graph walk
// from ever misreading a cache store's JSON.
func (s *Store) walkImageGraph(dgst, mediaType string, visit func(dgst string) error) error {
	if dgst == "" {
		return fmt.Errorf("the store carries an image descriptor with no digest (media type %q)", mediaType)
	}
	return s.walkImage(dgst, mediaType, visit, make(map[string]bool))
}

// walkImage is walkImageGraph's recursion; seen makes it terminate on a cyclic or duplicated graph.
func (s *Store) walkImage(dgst, mediaType string, visit func(dgst string) error, seen map[string]bool) error {
	if seen[dgst] {
		return nil
	}
	seen[dgst] = true
	if err := visit(dgst); err != nil {
		return err
	}
	switch mediaType {
	case MediaTypeOCIIndex, mediaTypeDockerList:
		raw, err := s.ReadBlob(dgst)
		if err != nil {
			return err
		}
		var nested storeIndex
		if err := json.Unmarshal(raw, &nested); err != nil {
			return fmt.Errorf("parse the image index %s: %w", dgst, err)
		}
		for _, child := range nested.Manifests {
			if err := s.walkImage(child.Digest, child.MediaType, visit, seen); err != nil {
				return err
			}
		}
	case mediaTypeOCIManifest, mediaTypeDockerManifest:
		raw, err := s.ReadBlob(dgst)
		if err != nil {
			return err
		}
		var manifest struct {
			Config indexDescriptor   `json:"config"`
			Layers []indexDescriptor `json:"layers"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return fmt.Errorf("parse the image manifest %s: %w", dgst, err)
		}
		if err := s.walkImage(manifest.Config.Digest, manifest.Config.MediaType, visit, seen); err != nil {
			return err
		}
		for _, layer := range manifest.Layers {
			if err := s.walkImage(layer.Digest, layer.MediaType, visit, seen); err != nil {
				return err
			}
		}
	}
	return nil
}
