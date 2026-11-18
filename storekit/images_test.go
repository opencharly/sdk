package storekit

// images_test.go — the CHARLY IMAGE STORE's properties, each arm keyed to the thing it would
// break in production:
//
//   - the RELOCATION and the root: the store lives under the DATA root, `CHARLY_CACHE_DIR` does
//     not move it, and `CHARLY_IMAGE_STORE_DIR` does (a check bed's build must not write the
//     operator's store);
//   - the NAMING ANNOTATION: `io.containerd.image.name` is the key, the tag-only
//     `org.opencontainers.image.ref.name` is not, and an archive that carries only the latter is
//     REFUSED rather than filed under a name nothing can resolve;
//   - the ONE-image-per-reference index: a rebuild replaces that reference's whole descriptor set
//     and leaves every other reference alone, concurrently;
//   - the ARCHIVE: it carries the marker, the annotated index and exactly the blobs the image
//     reaches — not the whole blob directory, which holds every image the host ever built.

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
// never accidentally share a blob.
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
	manifest := fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":%q,"size":%d},`+
			`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":%q,"size":%d}]}`,
		mediaTypeOCIManifest, configDigest, len(parts.config), layerDigest, len(parts.layer))
	parts.manifest = []byte(manifest)
	parts.digest, err = DigestOf(parts.manifest)
	if err != nil {
		t.Fatal(err)
	}
	return parts
}

// writeImageArchive writes a minimal OCI image archive for ref. `named` selects the annotations
// the index carries: the naming annotation (`io.containerd.image.name`, what a
// `--output type=oci,name=<full-ref>` export writes) plus the tag-only one, or — the CONTROL the
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

// TestImageStoreDirLivesUnderTheDataRootAndRelocates pins the ROOT and the ONE relocation. The
// data-root half is the point of opencharly/spec#372: the cache root is disposable by contract, so
// an image store there would be deleted by a cache clear.
func TestImageStoreDirLivesUnderTheDataRootAndRelocates(t *testing.T) {
	home := t.TempDir()
	pinRootsEnv(t, home)
	t.Setenv(ImageStoreDirEnv, "")

	dir, err := ImageStoreDir()
	if err != nil {
		t.Fatalf("ImageStoreDir: %v", err)
	}
	if want := filepath.Join(home, "xdg-data", "charly", "images"); dir != want {
		t.Errorf("ImageStoreDir() = %q, want %q under the DATA root", dir, want)
	}

	// The cache root's override must NOT carry the built images: an operator relocating a
	// disposable cache is not relocating a durable artefact.
	cache := filepath.Join(home, "relocated-cache")
	t.Setenv("CHARLY_CACHE_DIR", cache)
	if got, err := ImageStoreDir(); err != nil {
		t.Fatalf("ImageStoreDir with CHARLY_CACHE_DIR: %v", err)
	} else if strings.HasPrefix(got, cache) {
		t.Errorf("ImageStoreDir() = %q followed CHARLY_CACHE_DIR (%q); the data root must not follow the cache override", got, cache)
	}

	// The store's OWN relocation wins, and is used verbatim.
	relocated := filepath.Join(home, "bed-store")
	t.Setenv(ImageStoreDirEnv, relocated)
	if got, err := ImageStoreDir(); err != nil {
		t.Fatalf("ImageStoreDir with %s: %v", ImageStoreDirEnv, err)
	} else if got != relocated {
		t.Errorf("ImageStoreDir() = %q, want the %s override %q", got, ImageStoreDirEnv, relocated)
	}

	// An unset XDG_DATA_HOME falls back to $HOME/.local/share, through the contract resolver.
	t.Setenv(ImageStoreDirEnv, "")
	t.Setenv("XDG_DATA_HOME", "")
	if got, err := ImageStoreDir(); err != nil {
		t.Fatalf("ImageStoreDir with XDG_DATA_HOME unset: %v", err)
	} else if want := filepath.Join(home, ".local", "share", "charly", "images"); got != want {
		t.Errorf("ImageStoreDir() = %q with XDG_DATA_HOME unset, want %q", got, want)
	}
}

// TestImageStoreIsKeyedByTheNamingAnnotation is the annotation CONTROL at the store boundary: the
// full reference resolves, the tag alone does not, and an archive carrying only the tag-only
// annotation is refused instead of being filed under a key nothing can reach.
func TestImageStoreIsKeyedByTheNamingAnnotation(t *testing.T) {
	t.Setenv(ImageStoreDirEnv, t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}

	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	parts := addImage(t, store, ref, "layer-named")

	digest, ok, err := store.Lookup(ref)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", ref, err)
	}
	if !ok || digest != parts.digest {
		t.Fatalf("Lookup(%q) = (%q, %v), want the stored manifest digest %q", ref, digest, ok, parts.digest)
	}

	// The TAG ALONE is what `org.opencontainers.image.ref.name` carries, and it must not resolve:
	// keying on it would match the wrong image for every namespaced reference.
	if digest, ok, err := store.Lookup("2026.296.1200"); err != nil {
		t.Fatalf("Lookup(tag-only): %v", err)
	} else if ok {
		t.Errorf("Lookup(tag-only) found %q — the store must key on %s only", digest, AnnotationImageName)
	}

	// The CONTROL archive: the same image named only by `org.opencontainers.image.ref.name`, which
	// is exactly what a build export WITHOUT `name=` writes.
	control := writeImageArchive(t, filepath.Join(t.TempDir(), "control.tar"), "ghcr.io/opencharly/fedora:2026.296.1201",
		newImageParts(t, "layer-control"), false)
	err = store.Add(control, "ghcr.io/opencharly/fedora:2026.296.1201")
	if err == nil {
		t.Fatal("Add accepted an archive carrying no " + AnnotationImageName + " annotation — the entry would be unaddressable")
	}
	if !strings.Contains(err.Error(), AnnotationImageName) {
		t.Errorf("the refusal does not name the missing annotation %q: %v", AnnotationImageName, err)
	}
	if _, ok, _ := store.Lookup("ghcr.io/opencharly/fedora:2026.296.1201"); ok {
		t.Error("the refused archive was stored anyway")
	}
}

// TestImageStoreRefusesAnArchiveNamingAnotherReference: the store's key IS the reference the image
// was built as, so an archive naming one reference cannot be filed under another — a lookup would
// then materialise an image under a name it never had.
func TestImageStoreRefusesAnArchiveNamingAnotherReference(t *testing.T) {
	t.Setenv(ImageStoreDirEnv, t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const named, added = "ghcr.io/opencharly/fedora:2026.296.1200", "ghcr.io/opencharly/fedora:2026.296.1201"
	path := writeImageArchive(t, filepath.Join(t.TempDir(), "export.tar"), named, newImageParts(t, "layer-mismatch"), true)

	err = store.Add(path, added)
	if err == nil {
		t.Fatalf("Add accepted an archive naming %q as %q", named, added)
	}
	if !strings.Contains(err.Error(), named) || !strings.Contains(err.Error(), added) {
		t.Errorf("the refusal names neither the archive's reference nor the requested one: %v", err)
	}
}

// TestImageStoreReplacesOneReferenceAndKeepsEveryOther proves the index operation: a rebuild of ONE
// reference replaces that reference's descriptor set, keeps every other reference byte-for-byte,
// and leaves the superseded image's blobs content-addressed in the store (nothing is rewritten, and
// nothing another reference shares is lost).
func TestImageStoreReplacesOneReferenceAndKeepsEveryOther(t *testing.T) {
	t.Setenv(ImageStoreDirEnv, t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const rebuilt, other = "ghcr.io/opencharly/fedora:2026.296.1200", "ghcr.io/opencharly/arch:2026.296.1201"

	first := addImage(t, store, rebuilt, "layer-first")
	otherParts := addImage(t, store, other, "layer-other")
	second := addImage(t, store, rebuilt, "layer-second")

	if digest, ok, _ := store.Lookup(rebuilt); !ok || digest != second.digest {
		t.Errorf("Lookup(%q) = (%q, %v), want the REBUILD's manifest %q", rebuilt, digest, ok, second.digest)
	}
	if digest, ok, _ := store.Lookup(other); !ok || digest != otherParts.digest {
		t.Errorf("the rebuild disturbed the unrelated reference: Lookup(%q) = (%q, %v), want %q", other, digest, ok, otherParts.digest)
	}
	refs, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 2 || refs[0] != other || refs[1] != rebuilt {
		t.Errorf("List() = %v, want [%q %q] — one reference per image, sorted", refs, other, rebuilt)
	}

	// The superseded image's layer is still there: content-addressed bytes are immutable, and the
	// sweep that reclaims an unreferenced one is a separate, deliberate call.
	layerDigest, err := DigestOf(first.layer)
	if err != nil {
		t.Fatal(err)
	}
	if !store.store.HasBlob(layerDigest) {
		t.Errorf("the superseded layer %s was removed by the rebuild", layerDigest)
	}

	// The layout marker is what makes the directory an OCI layout to another process (a solve gets
	// it as `--oci-layout charly=<dir>`), so it must exist after a write.
	marker, err := os.ReadFile(filepath.Join(store.Dir(), LayoutFile)) // #nosec G304 — the store's own path.
	if err != nil {
		t.Fatalf("read the layout marker: %v", err)
	}
	if string(marker) != layoutMarker {
		t.Errorf("%s = %q, want %q", LayoutFile, marker, layoutMarker)
	}
}

// TestConcurrentImageStoreAddsKeepEveryReference is the cross-writer index test: two levels of one
// build (and two builds at once) write the store concurrently, so the read-modify-write must be
// serialized or one writer's reference is silently DROPPED — data loss, not a spurious error.
func TestConcurrentImageStoreAddsKeepEveryReference(t *testing.T) {
	t.Setenv(ImageStoreDirEnv, t.TempDir())
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

	refs_after, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs_after) != writers {
		t.Fatalf("List() has %d references, want all %d — a concurrent writer's entry was lost", len(refs_after), writers)
	}
	for _, want := range refs {
		found := false
		for _, got := range refs_after {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the store lost reference %q: %v", want, refs_after)
		}
	}
}

// TestImageStoreArchiveCarriesTheNamingAnnotationAndOnlyItsBlobs is the ARCHIVE contract: the
// marker, an index whose descriptor names the full reference, and exactly the blobs that image
// reaches. A copy of `blobs/` would carry every other image the host ever built — which on this
// host is 619 GiB of them.
func TestImageStoreArchiveCarriesTheNamingAnnotationAndOnlyItsBlobs(t *testing.T) {
	t.Setenv(ImageStoreDirEnv, t.TempDir())
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	parts := addImage(t, store, ref, "layer-archived")
	// An unrelated image, whose blobs must NOT appear in the archive of ref.
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
	// The unrelated image's MANIFEST and LAYER must not travel with ref. (Its CONFIG is the same
	// bytes as ref's — `{"architecture":"amd64","os":"linux"}` — so the two legitimately share ONE
	// content-addressed blob; that is the store's "cached once" property, not a leak.)
	for _, unwanted := range [][]byte{unrelated.manifest, unrelated.layer} {
		if name := blobMember(t, unwanted); members[name] != nil {
			t.Errorf("the archive carries %s, which belongs to another image", name)
		}
	}
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
	t.Setenv(ImageStoreDirEnv, t.TempDir())
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
	t.Setenv(ImageStoreDirEnv, filepath.Join(root, "store"))
	store, err := OpenImageStore()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ghcr.io/opencharly/fedora:2026.296.1200"
	parts := newImageParts(t, "layer-escape")
	path := writeImageArchive(t, filepath.Join(t.TempDir(), "export.tar"), ref, parts, true)

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
