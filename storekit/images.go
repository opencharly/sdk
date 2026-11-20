package storekit

// images.go — THE CHARLY IMAGE STORE: the engine-neutral artefact of every build.
//
// The store is the `registry` NAMED STORE under the ONE cache root — `<cache root>/registry`,
// addressed through `StoreDir` — and every decision below is a contract row:
//
//   - D-STORE-1: one root, no image-store override. Named stores live under the cache root, so a
//     disposable target isolates with the cache root's overrides (`CHARLY_CACHE_HOME`, then the
//     compatibility `CHARLY_CACHE_DIR`) and this store follows them. There is NO separate
//     image-store override: a bed that relocated only images would be a second root resolver
//     wearing an environment variable.
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
//	Key         = the artifact's own digest        (D-LAW-7: an artifact's name IS a digest)
//	Payload     = the same digest                  (the entry's payload is its root document)
//	MediaType   = what that document is
//	Annotations = the reference + the build time   (the RESOLUTION RECORD, outside the payload)
//
// The reference is an ANNOTATION, never the key. D-STORE-5 is satisfied in the only way a store
// can satisfy it: the mutable name is resolved to the artifact's digest BEFORE the write, and the
// digest is what the store keys on; a reader resolves a name to a digest by reading the entries'
// annotations, and the digest — not the name — is what it then asks the store for.
//
// A REBUILD DELETES NOTHING. The new artifact is a different digest, so it is a different key: the
// store APPENDS it and the previous artifact remains a first-class entry, addressable by its own
// digest (D-STORE-7's "keeps what state references and evicts the rest" is a decision for the
// sweep, which can see both entries — key, reference and build time — and reclaim the superseded
// one with `DeleteEntry` + `UnreferencedBlobs`). Replacing an entry for a reference, which is what
// this store did before this change, destroyed exactly the evidence a sweep needs.
//
// D-FORM-2's key — "content-addressed by the digests of its start artifact and its IR" — is the
// same digest seen from the other side: for a reproducible build the artifact's content address
// IS the function of those inputs. This store cannot compute the input key before the build, and
// it does not pretend to: it keys on the digest the build produced, which is what a reader and a
// sweep actually need.
//
// The entry model, the annotation slot and the "payload outside the digest" split are the store's
// own (`Entry`'s doc), not a shape invented here — which is why a built image is the same kind of
// entry opencharly/plugin-cache's `registry` ingest writes.
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

// Lookup returns the digest ref currently resolves to inside the store — the newest entry whose
// annotation names it.
func (s *ImageStore) Lookup(ref string) (digest string, ok bool, err error) {
	entry, found, err := s.newestEntry(ref)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, nil
	}
	return entry.Key, true, nil
}

// Has reports whether the store holds ref.
func (s *ImageStore) Has(ref string) (bool, error) {
	_, ok, err := s.newestEntry(ref)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// newestEntry is the ONE resolver from a reference to the entry it names. The reference is not a
// key, so it is answered from the entries' annotations: the matching entry with the greatest
// build time, and on a tie the LATER in index order — which is write order, because a rebuild
// appends. One index scan and one small read per entry, which is the price of keying by identity
// instead of by name; the store's entries are its inventory, not a sidecar.
func (s *ImageStore) newestEntry(ref string) (*Entry, bool, error) {
	idx, err := s.store.readIndex()
	if err != nil {
		return nil, false, err
	}
	var best *Entry
	var bestCreated time.Time
	for _, d := range idx.Manifests {
		raw, rerr := s.store.ReadBlob(d.Digest)
		if rerr != nil {
			return nil, false, rerr
		}
		var e Entry
		if uerr := json.Unmarshal(raw, &e); uerr != nil {
			return nil, false, fmt.Errorf("parse the entry %s: %w", d.Digest, uerr)
		}
		if e.Annotations[AnnotationImageName] != ref {
			continue
		}
		created, _ := time.Parse(time.RFC3339, e.Annotations[AnnotationCreated])
		if best == nil || !created.Before(bestCreated) {
			best, bestCreated = &e, created
		}
	}
	if best == nil {
		return nil, false, nil
	}
	return best, true, nil
}

// List returns every reference the store holds, deduplicated and sorted: the annotations the
// entries carry, not their keys — a caller elects a tag over references, and a rebuild leaves two
// entries naming one reference.
//
// The KEYS are the store's inventory, and a sweep reads them through `Entries()` (key = digest,
// plus the reference and the build time), which is the surface D-STORE-7's "know what state
// references" needs.
func (s *ImageStore) List() ([]string, error) {
	entries, err := s.store.Entries()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(entries))
	refs := make([]string, 0, len(entries))
	for _, e := range entries {
		ref := e.Annotations[AnnotationImageName]
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs, nil
}

// Add lands an exported OCI archive as ONE store entry, KEYED BY THE ARTIFACT'S DIGEST and
// annotated with the reference it was built as.
//
// The order is the contract: blobs first, the entry LAST, so a reader never observes a key whose
// payload is not yet published. Existing blobs are not rewritten (content-addressed bytes are the
// same bytes), and the entry's payload is the archive's own `index.json` — the OCI layout's entry
// point — so a single-platform and a multi-platform export are stored the same way and `Archive`
// reproduces either.
//
// Nothing is DELETED here: a rebuild produces a different digest, hence a different key, hence a
// new entry beside the old one — which stays reachable by its own digest until a sweep reclaims
// it. That is what makes the reference an annotation rather than an identity: replacing the entry
// for a name would have thrown away the previous artifact's address.
//
// The archive must ALREADY name ref with the export annotation, because the same file is what an
// engine load reads; a caller that leaned on the store to supply the name would still be feeding
// an unaddressable archive to that load. The payload is published with that annotation STRIPPED
// (D-STORE-6: a payload carries no name and no timestamp); the ENTRY carries both, where the
// store's own model puts a resolution record, and `Archive` re-applies the export annotation —
// the one surface that crosses the engine boundary.
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
	// THE ENTRY: keyed by the digest the build produced, carrying the reference and the build time
	// as annotations — the resolution record D-STORE-5 asks for, beside the key and outside the
	// payload's digest by construction.
	// The build time is REQUIRED on the entry, because it is what makes "which artifact does this
	// reference mean NOW" answerable without a sidecar: the export's own time when it carries one,
	// otherwise the moment the store learned of it. Either way it is recorded on the ENTRY, never
	// in the payload (D-STORE-6).
	created := exportCreated(exported)
	if created == "" {
		created = time.Now().UTC().Format(time.RFC3339)
	}
	annotations := map[string]string{
		AnnotationImageName: ref,
		AnnotationCreated:   created,
	}
	if _, err := s.store.PutEntry(Entry{
		Key:         payloadDigest,
		MediaType:   indexMediaType(exported),
		Payload:     payloadDigest,
		Annotations: annotations,
	}); err != nil {
		return fmt.Errorf("index %s in the %s store: %w", ref, ImageStoreName, err)
	}
	return nil
}

// exportCreated returns the build time the export carries, if any. It is re-recorded on the ENTRY
// (never in the payload) because it is what makes "which artifact does this reference mean NOW"
// answerable — D-STORE-6's one sanctioned home for a resolution time.
func exportCreated(exported *storeIndex) string {
	for _, d := range exported.Manifests {
		if created := d.Annotations[AnnotationCreated]; created != "" {
			return created
		}
	}
	return ""
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
	entry, ok, err := s.newestEntry(ref)
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
