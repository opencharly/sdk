package storekit

// images_test.go — the CHARLY IMAGE STORE's properties, each arm keyed to the contract row it
// would break:
//
//   - D-STORE-1 — the store is the named `registry` store under the ONE cache root, relocated by
//     `CHARLY_CACHE_DIR` and by nothing else;
//   - D-STORE-3 — one content-addressed store holds the artifact too, its tree is the layout and
//     nothing else, and a rebuild moves the INDEX POINTER rather than mutating an entry;
//   - D-STORE-5 — the name a caller asks by is the entry's KEY; the artifact is stored and served
//     by the digest it resolved to;
//   - D-STORE-7 — a live artifact keeps its whole graph reachable, the store is discoverable by
//     `NamedStores()`, and a superseded or deleted artifact's blobs become exactly the sweep's
//     candidates;
//   - D-STORE-6 — a stored payload carries no name and no timestamp;
//   - and the export contract: an archive crossing the engine boundary carries
//     `io.containerd.image.name`.

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// imageParts is the content of one minimal OCI image: a config blob, a layer blob, the manifest
// that names both, and the digest of that manifest.
type imageParts struct {
	config   []byte
	layer    []byte
	manifest []byte
	digest   string
}

// newImageParts builds a minimal image whose layer bytes are distinct per call, so two "builds"
// never accidentally share a layer.
func newImageParts(t *testing.T, layer string) imageParts {
	t.Helper()
	parts := imageParts{
		config: []byte(`{"architecture":"amd64","os":"linux"}`),
		layer:  []byte(layer),
	}
	configDigest, err := DigestOf(parts.config)
	if err != nil {
		t.Fatal(err)
	}
	layerDigest, err := DigestOf(parts.layer)
	if err != nil {
		t.Fatal(err)
	}
	parts.manifest = []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":%q,"size":%d},`+
			`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":%q,"size":%d}]}`,
		mediaTypeOCIManifest, configDigest, len(parts.config), layerDigest, len(parts.layer)))
	parts.digest, err = DigestOf(parts.manifest)
	if err != nil {
		t.Fatal(err)
	}
	return parts
}

// writeImageArchive writes a minimal OCI image archive for ref. `named` selects the annotations
// the index carries: the export annotation (`io.containerd.image.name`, what a
// `--output type=oci,name=<full-ref>` build writes) plus the tag-only one, or — the CONTROL a
// `name=`-less export produces — the tag-only annotation alone.
func writeImageArchive(t *testing.T, path, ref string, parts imageParts, named bool) string {
	t.Helper()
	annotations := map[string]string{AnnotationRefName: ref[strings.LastIndexByte(ref, ':')+1:]}
	if named {
		annotations[AnnotationImageName] = ref
	}
	index, err := json.Marshal(&storeIndex{
		SchemaVersion: 2,
		MediaType:     MediaTypeOCIIndex,
		Manifests: []indexDescriptor{{
			MediaType:   mediaTypeOCIManifest,
			Digest:      parts.digest,
			Size:        int64(len(parts.manifest)),
			Annotations: annotations,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path) // #nosec G304 — a test path this file chose.
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	members := []struct {
		name string
		data []byte
	}{
		{LayoutFile, []byte(layoutMarker)},
		{indexFile, index},
		{blobMember(t, parts.manifest), parts.manifest},
		{blobMember(t, parts.config), parts.config},
		{blobMember(t, parts.layer), parts.layer},
	}
	for _, m := range members {
		if err := tarFile(tw, m.name, bytes.NewReader(m.data), int64(len(m.data))); err != nil {
			t.Fatalf("write %s: %v", m.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// blobMember renders the archive member name of a blob, independently of blobMemberName, so a
// regression in that helper cannot hide behind a test that used it.
func blobMember(t *testing.T, data []byte) string {
	t.Helper()
	dgst, err := DigestOf(data)
	if err != nil {
		t.Fatal(err)
	}
	return blobDir + "/" + strings.Replace(dgst, ":", "/", 1)
}

// addImage is the one-line production path: export, then store.
func addImage(t *testing.T, store *ImageStore, ref, layer string) imageParts {
	t.Helper()
	parts := newImageParts(t, layer)
	path := writeImageArchive(t, filepath.Join(t.TempDir(), "export.tar"), ref, parts, true)
	if err := store.Add(path, ref); err != nil {
		t.Fatalf("Add(%q): %v", ref, err)
	}
	return parts
}

// unresolvable is the set of blob digests a sweep would reclaim, as a set for comparison.
func unresolvable(t *testing.T, store *ImageStore) map[string]bool {
	t.Helper()
	blobs, err := store.store.UnreferencedBlobs()
	if err != nil {
		t.Fatalf("UnreferencedBlobs: %v", err)
	}
	out := make(map[string]bool, len(blobs))
	for _, b := range blobs {
		out[b.Digest] = true
	}
	return out
}

// blobSet is the set of an image's own blob digests (manifest, config, layer).
func blobSet(t *testing.T, parts imageParts) map[string]bool {
	t.Helper()
	out := make(map[string]bool, 3)
	for _, data := range [][]byte{parts.manifest, parts.config, parts.layer} {
		dgst, err := DigestOf(data)
		if err != nil {
			t.Fatal(err)
		}
		out[dgst] = true
	}
	return out
}

// TestImageStoreIsTheRegistryStoreUnderTheCacheRoot pins D-STORE-1: the store is a NAMED store
// under the ONE cache root, so the single override relocates it — and the retired
// `CHARLY_IMAGE_STORE_DIR` no longer does anything (R5: the second relocation lever is gone, not
// left dormant).
func TestImageStoreIsTheRegistryStoreUnderTheCacheRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	t.Setenv("CHARLY_IMAGE_STORE_DIR", "")

	dir, err := ImageStoreDir()
	if err != nil {
		t.Fatalf("ImageStoreDir: %v", err)
	}
	if want := filepath.Join(root, "registry"); dir != want {
		t.Errorf("ImageStoreDir() = %q, want the named registry store %q under the cache root", dir, want)
	}

	// The retired lever must be INERT: a stale environment variable may not move the store to a
	// second root (that is the defect the fifth root's revert removed).
	t.Setenv("CHARLY_IMAGE_STORE_DIR", filepath.Join(root, "elsewhere"))
	if got, err := ImageStoreDir(); err != nil {
		t.Fatalf("ImageStoreDir: %v", err)
	} else if got != dir {
		t.Errorf("ImageStoreDir() = %q with the retired CHARLY_IMAGE_STORE_DIR set, want %q — the cache root's override is the ONE override", got, dir)
	}
}

// TestImageStoreIsKeyedByTheArtifactDigestAndAnnotatedWithTheReference is the KEYING claim
// (D-STORE-5, D-STORE-3, D-LAW-7): the store keys an artifact by the DIGEST it resolved to, the
// reference rides as an annotation, and the stored payload carries neither a name nor a timestamp
// (D-STORE-6).
func TestImageStoreIsKeyedByTheArtifactDigestAndAnnotatedWithTheReference(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	parts := addImage(t, store, ref, "layer-entry")

	digest, ok, err := store.Lookup(ref)
	if err != nil || !ok {
		t.Fatalf("Lookup(%q) = (_, %v, %v)", ref, ok, err)
	}
	// The KEY is the digest, and it is the same value the payload names: an artifact's identity in
	// this store is its content, never the reference it was built as.
	entry, found, err := store.store.LookupEntry(digest)
	if err != nil || !found {
		t.Fatalf("LookupEntry(%q) = (_, %v, %v) — the store must be addressable BY DIGEST", digest, found, err)
	}
	if entry.Key != digest || entry.Payload != digest {
		t.Errorf("entry = {Key:%q, Payload:%q}, want both to be the artifact digest %q", entry.Key, entry.Payload, digest)
	}
	if got := entry.Annotations[AnnotationImageName]; got != ref {
		t.Errorf("the entry's %s annotation = %q, want the reference %q", AnnotationImageName, got, ref)
	}
	if entry.MediaType != MediaTypeOCIIndex {
		t.Errorf("entry.MediaType = %q, want the artifact index media type %q", entry.MediaType, MediaTypeOCIIndex)
	}

	// The payload IS the artifact's content address, and it holds no name and no timestamp.
	payload, err := store.store.ReadBlob(entry.Payload)
	if err != nil {
		t.Fatalf("read the payload %s: %v", entry.Payload, err)
	}
	if got, derr := DigestOf(payload); derr != nil {
		t.Fatal(derr)
	} else if got != entry.Payload {
		t.Errorf("entry.Payload = %q is not the content address of the bytes it names", entry.Payload)
	}
	var stored storeIndex
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatalf("parse the stored index: %v", err)
	}
	if len(stored.Manifests) != 1 || stored.Manifests[0].Digest != parts.digest {
		t.Fatalf("the stored index names %+v, want the built manifest %q", stored.Manifests, parts.digest)
	}
	for _, banned := range []string{AnnotationImageName, AnnotationRefName, AnnotationCreated} {
		if _, present := stored.Manifests[0].Annotations[banned]; present {
			t.Errorf("the stored payload carries the %q annotation; a payload holds no name and no timestamp", banned)
		}
	}

	// The reference is what a caller asks by, and the TAG ALONE is not a reference.
	if _, ok, err := store.Lookup("2026.296.1200"); err != nil || ok {
		t.Errorf("Lookup(tag-only) = (_, %v, %v) — the tag alone names nothing", ok, err)
	}
	if held, err := store.Has("2026.296.1200"); err != nil || held {
		t.Errorf("Has(tag-only) = (%v, %v) — the tag alone names nothing", held, err)
	}
	refs, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 || refs[0] != ref {
		t.Errorf("List() = %v, want [%q]", refs, ref)
	}
}

// TestImageStoreIsDiscoveredByNamedStores pins D-STORE-7's precondition: a store the sweep cannot
// discover is a store that grows forever. The `oci-layout` marker is written by the store's own
// index publication, so every storekit store is discoverable.
func TestImageStoreIsDiscoveredByNamedStores(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	addImage(t, store, "ghcr.io/opencharly/fedora:2026.296.1200", "layer-discoverable")

	names, err := NamedStores()
	if err != nil {
		t.Fatalf("NamedStores: %v", err)
	}
	if len(names) != 1 || names[0] != ImageStoreName {
		t.Fatalf("NamedStores() = %v, want [%q] — a store the sweep cannot see is never swept", names, ImageStoreName)
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), LayoutFile)); err != nil {
		t.Fatalf("the store carries no %s marker: %v", LayoutFile, err)
	}
}

// TestImageStoreLiveArtifactKeepsItsWholeGraphReachable is D-STORE-7 applied to an artifact: a
// live image's CONFIG and LAYERS are reachable only through its root document, so a sweep that
// marked the root alone would keep a manifest and delete the layer it names.
func TestImageStoreLiveArtifactKeepsItsWholeGraphReachable(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	parts := addImage(t, store, "ghcr.io/opencharly/fedora:2026.296.1200", "layer-live")

	if sweepable := unresolvable(t, store); len(sweepable) != 0 {
		t.Fatalf("the sweep would reclaim %d blob(s) of a LIVE image: %v", len(sweepable), sweepable)
	}
	for dgst := range blobSet(t, parts) {
		if !store.store.HasBlob(dgst) {
			t.Errorf("the live image's blob %s is not in the store", dgst)
		}
	}
}

// TestImageStoreRebuildAppendsAndKeepsTheOldArtifactAddressable is the IDENTITY behaviour the
// ruling turns on: a rebuild deletes NOTHING. The new artifact is a new digest, so the store
// appends a second entry; the previous artifact remains a first-class entry addressable by its own
// digest, and reclaiming it is a SWEEP decision (D-STORE-7) made from what `Entries()` exposes —
// key, reference and build time — never something a build performs.
func TestImageStoreRebuildAppendsAndKeepsTheOldArtifactAddressable(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	first := addImage(t, store, ref, "layer-first")
	firstDigest, _, err := store.Lookup(ref)
	if err != nil {
		t.Fatal(err)
	}
	second := addImage(t, store, ref, "layer-second")
	secondDigest, ok, err := store.Lookup(ref)
	if err != nil || !ok {
		t.Fatalf("Lookup after the rebuild = (_, %v, %v)", ok, err)
	}
	if firstDigest == secondDigest {
		t.Fatal("the rebuild did not produce a new artifact digest")
	}

	// THE OLD ARTIFACT IS STILL AN ENTRY, by its own digest — not merely bytes on disk.
	if _, found, err := store.store.LookupEntry(firstDigest); err != nil || !found {
		t.Fatalf("the superseded artifact %s is no longer addressable by digest (found=%v, err=%v)", firstDigest, found, err)
	}
	// Two entries name the reference; the store's LIST is references, so it stays one.
	entries, err := store.store.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	named := 0
	for _, e := range entries {
		if e.Annotations[AnnotationImageName] == ref {
			named++
		}
	}
	if named != 2 {
		t.Errorf("%d entr(ies) name %q, want 2 — a rebuild APPENDS, it does not replace", named, ref)
	}
	if refs, err := store.List(); err != nil {
		t.Fatal(err)
	} else if len(refs) != 1 || refs[0] != ref {
		t.Errorf("List() = %v after a rebuild, want one reference %q", refs, ref)
	}
	// Both artifacts are live, so the sweep must reclaim nothing yet.
	if sweepable := unresolvable(t, store); len(sweepable) != 0 {
		t.Fatalf("the sweep would reclaim %d blob(s) of two LIVE artifacts: %v", len(sweepable), sweepable)
	}

	// A sweep keeps what state references (the newest artifact for the reference) and evicts the
	// rest: `DeleteEntry` names the digest, and the superseded artifact's own blobs then become
	// exactly the sweep's candidates.
	if removed, err := store.store.DeleteEntry(firstDigest); err != nil || !removed {
		t.Fatalf("DeleteEntry(%q) = (%v, %v)", firstDigest, removed, err)
	}
	sweepable := unresolvable(t, store)
	for _, data := range [][]byte{first.manifest, first.layer} {
		dgst, derr := DigestOf(data)
		if derr != nil {
			t.Fatal(derr)
		}
		if !sweepable[dgst] {
			t.Errorf("the superseded blob %s is not named by the sweep", dgst)
		}
	}
	// ... its CONFIG is not, because both builds share those identical bytes and the image that is
	// still current keeps them (the store's "cached once" property, not a leak) ...
	if dgst, _ := DigestOf(first.config); sweepable[dgst] {
		t.Errorf("the sweep would reclaim the config blob %s, which the CURRENT image still reaches", dgst)
	}
	// ... and nothing the current artifact reaches may be named.
	for dgst := range blobSet(t, second) {
		if sweepable[dgst] {
			t.Errorf("the sweep would reclaim the CURRENT blob %s", dgst)
		}
	}
	if sweepable[secondDigest] {
		t.Errorf("the sweep would reclaim the current artifact %s", secondDigest)
	}
	for dgst := range sweepable {
		if err := store.store.RemoveBlob(dgst); err != nil {
			t.Fatalf("RemoveBlob(%s): %v", dgst, err)
		}
	}
	if left := unresolvable(t, store); len(left) != 0 {
		t.Fatalf("after the sweep %d blob(s) are STILL unreferenced: %v", len(left), left)
	}
	live := blobSet(t, second)
	live[secondDigest] = true
	for dgst := range live {
		if !store.store.HasBlob(dgst) {
			t.Errorf("the sweep removed %s, which the live entry reaches", dgst)
		}
	}
}

// TestImageStoreAddRefusesAnUnnamedOrMismatchedArchive: the archive must ALREADY name the
// reference it is added as — the same file is what an engine load reads, and an entry filed under
// a reference it does not name would be resolved and materialised as the wrong image.
func TestImageStoreAddRefusesAnUnnamedOrMismatchedArchive(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}

	// The CONTROL archive: named only by `org.opencontainers.image.ref.name`, which is what a build
	// export WITHOUT `name=` writes.
	control := writeImageArchive(t, filepath.Join(t.TempDir(), "control.tar"),
		"ghcr.io/opencharly/fedora:2026.296.1201", newImageParts(t, "layer-control"), false)
	err = store.Add(control, "ghcr.io/opencharly/fedora:2026.296.1201")
	if err == nil {
		t.Fatal("Add accepted an archive carrying no " + AnnotationImageName + " annotation")
	}
	if !strings.Contains(err.Error(), AnnotationImageName) {
		t.Errorf("the refusal does not name the missing annotation %q: %v", AnnotationImageName, err)
	}

	const named, added = "ghcr.io/opencharly/fedora:2026.296.1200", "ghcr.io/opencharly/fedora:2026.296.1299"
	mismatched := writeImageArchive(t, filepath.Join(t.TempDir(), "mismatch.tar"), named, newImageParts(t, "layer-mismatch"), true)
	err = store.Add(mismatched, added)
	if err == nil {
		t.Fatalf("Add accepted an archive naming %q as %q", named, added)
	}
	if !strings.Contains(err.Error(), named) || !strings.Contains(err.Error(), added) {
		t.Errorf("the refusal names neither the archive's reference nor the requested one: %v", err)
	}
}

// TestConcurrentImageStoreAddsKeepEveryReference is the cross-writer test: two builds (and two
// levels of one build) write the store concurrently, so the entry RMW must be serialized or one
// writer's reference is silently DROPPED — data loss, not a spurious error.
func TestConcurrentImageStoreAddsKeepEveryReference(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", t.TempDir())
	const writers = 8
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}

	paths := make([]string, writers)
	refs := make([]string, writers)
	for i := range writers {
		refs[i] = fmt.Sprintf("ghcr.io/opencharly/concurrent:%d", 20262961200+i)
		paths[i] = writeImageArchive(t, filepath.Join(t.TempDir(), "export.tar"), refs[i],
			newImageParts(t, fmt.Sprintf("layer-%d", i)), true)
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.Add(paths[i], refs[i])
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Add(%q): %v", refs[i], err)
		}
	}

	after, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(after) != writers {
		t.Fatalf("List() has %d references, want all %d — a concurrent writer's entry was lost", len(after), writers)
	}
	for _, want := range refs {
		found := false
		for _, got := range after {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the store lost reference %q: %v", want, after)
		}
	}
}

// TestImageStoreArchiveCarriesTheNamingAnnotationAndOnlyItsBlobs is the ARCHIVE contract: the
// marker, an index whose descriptor names the full reference, and exactly the blobs that image
// reaches. A copy of `blobs/` would carry every other image the host ever built.
func TestImageStoreArchiveCarriesTheNamingAnnotationAndOnlyItsBlobs(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	parts := addImage(t, store, ref, "layer-archived")
	// An unrelated image, whose MANIFEST and LAYER must not appear in the archive of ref.
	unrelated := addImage(t, store, "ghcr.io/opencharly/arch:2026.296.1201", "layer-unrelated")

	var archive bytes.Buffer
	if err := store.WriteArchive(ref, &archive); err != nil {
		t.Fatalf("WriteArchive(%q): %v", ref, err)
	}

	members := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(archive.Bytes()))
	for {
		hdr, rerr := tr.Next()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			t.Fatalf("read the archive: %v", rerr)
		}
		raw, rerr := io.ReadAll(tr)
		if rerr != nil {
			t.Fatalf("read %s: %v", hdr.Name, rerr)
		}
		if _, dup := members[hdr.Name]; dup {
			t.Fatalf("the archive lists %s twice", hdr.Name)
		}
		members[hdr.Name] = raw
	}

	if got := string(members[LayoutFile]); got != layoutMarker {
		t.Errorf("archive %s = %q, want %q", LayoutFile, got, layoutMarker)
	}
	var idx storeIndex
	if err := json.Unmarshal(members[indexFile], &idx); err != nil {
		t.Fatalf("parse the archive's %s: %v", indexFile, err)
	}
	if len(idx.Manifests) != 1 {
		t.Fatalf("the archive's index carries %d manifests, want the one stored image", len(idx.Manifests))
	}
	if got := idx.Manifests[0].Annotations[AnnotationImageName]; got != ref {
		t.Errorf("the archive's index names %q, want the full reference %q — an archive without it loads untagged", got, ref)
	}
	for _, want := range [][]byte{parts.manifest, parts.config, parts.layer} {
		name := blobMember(t, want)
		got, ok := members[name]
		if !ok {
			t.Errorf("the archive is missing blob %s", name)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("archive member %s does not hold the stored bytes", name)
		}
	}
	for _, unwanted := range [][]byte{unrelated.manifest, unrelated.layer} {
		if name := blobMember(t, unwanted); members[name] != nil {
			t.Errorf("the archive carries %s, which belongs to another image", name)
		}
	}
	// The two images share the identical CONFIG bytes, so ONE content-addressed blob serves both —
	// the store's "cached once" property, not a leak.
	if !bytes.Equal(members[blobMember(t, unrelated.config)], unrelated.config) {
		t.Error("the archive is missing the config blob the two images share")
	}

	// Determinism: the store is immutable content-addressed bytes, so two archives of one image are
	// the same bytes — nothing (no timestamp, no map order) may vary per call.
	var again bytes.Buffer
	if err := store.WriteArchive(ref, &again); err != nil {
		t.Fatalf("WriteArchive(%q) again: %v", ref, err)
	}
	if !bytes.Equal(archive.Bytes(), again.Bytes()) {
		t.Error("two archives of the same stored image differ — the archive is not a function of the store")
	}

	if err := store.WriteArchive("ghcr.io/opencharly/fedora:absent", io.Discard); err == nil {
		t.Error("WriteArchive succeeded for a reference the store does not hold")
	}
}

// TestImageStoreArchiveMaterialisesACallerOwnedFile: `Archive` stages a real file (what an engine
// `load -i` needs) and hands its ownership to the caller.
func TestImageStoreArchiveMaterialisesACallerOwnedFile(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	addImage(t, store, ref, "layer-file")

	path, err := store.Archive(ref)
	if err != nil {
		t.Fatalf("Archive(%q): %v", ref, err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	raw, err := os.ReadFile(path) // #nosec G304 — the path Archive just returned.
	if err != nil {
		t.Fatalf("read the materialised archive: %v", err)
	}
	var names []string
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, rerr := tr.Next()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			t.Fatalf("read the materialised archive: %v", rerr)
		}
		names = append(names, hdr.Name)
	}
	if len(names) < 3 || names[0] != LayoutFile || names[1] != indexFile {
		t.Errorf("materialised archive members = %v, want %s then %s then the blobs", names, LayoutFile, indexFile)
	}
}

// TestImageStoreRefusesAnUnsafeBlobPathInAnArchive: an archive's member names are DATA. A name
// that would leave `blobs/` must be refused, and nothing may be written outside the store tree.
func TestImageStoreRefusesAnUnsafeBlobPathInAnArchive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	path := writeImageArchive(t, filepath.Join(t.TempDir(), "export.tar"), ref, newImageParts(t, "layer-escape"), true)

	// Append the traversal member to a copy of a valid export.
	f, err := os.Open(path) // #nosec G304 — a test path this file chose.
	if err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "escape.tar")
	out, err := os.Create(bad) // #nosec G304 — a test path this file chose.
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(out)
	tr := tar.NewReader(f)
	for {
		hdr, rerr := tr.Next()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			t.Fatal(rerr)
		}
		raw, rerr := io.ReadAll(tr)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if err := tarFile(tw, hdr.Name, bytes.NewReader(raw), int64(len(raw))); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("escaped")
	if err := tarFile(tw, "blobs/../../escaped", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.Add(bad, ref); err == nil {
		t.Fatal("Add accepted an archive carrying a blob path that escapes the store tree")
	}
	if _, err := os.Stat(filepath.Join(root, "escaped")); err == nil {
		t.Fatal("the traversal member was written outside the store's blob tree")
	}
}
