package deploykit

// image_charly_test.go — the PRODUCTION CharlyImageSource, driven END TO END: a real charly image
// store on disk, a real OCI export added to it, and the resolver importing the archive the source
// materialises. Nothing about the layout is re-implemented here — the store under test is the one
// `sdk/storekit` owns, which is the point of the move.
//
// Two boundaries are asserted at the artifact itself rather than through the source's own word:
//
//   - the ARCHIVE the source returns is read back as a tar, so "it carries
//     io.containerd.image.name" is a statement about bytes an engine would load;
//   - the file the resolver asked for is GONE once the resolve returns, so a successful resolve
//     cannot quietly leave a copy of the image on the operator's disk.

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/storekit"
)

// recordingStore is an ImageStore that remembers what it was handed: the resolver's import path
// is only provable at the boundary it crosses.
type recordingStore struct {
	present  map[string]bool
	imported []string
}

func newRecordingStore() *recordingStore { return &recordingStore{present: map[string]bool{}} }

func (s *recordingStore) Has(_ context.Context, ref string) (bool, error) { return s.present[ref], nil }

func (s *recordingStore) Import(_ context.Context, ociSource, ref string) error {
	s.imported = append(s.imported, ociSource)
	s.present[ref] = true
	return nil
}

func (s *recordingStore) Pull(_ context.Context, ref string) error { s.present[ref] = true; return nil }

func (s *recordingStore) Tag(_ context.Context, _, _ string) error { return nil }

func (s *recordingStore) Remove(_ context.Context, _ ...string) error { return nil }

func (s *recordingStore) List(_ context.Context) ([]string, error) { return nil, nil }

// seedImageStore puts ONE image into a real charly image store at dir, through the store's own
// Add: the store is production code, not a fixture of it.
func seedImageStore(t *testing.T, dir, ref string) {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "export.tar")
	writeNamedExport(t, archive, ref)
	if err := storekit.OpenImageStoreDir(dir).Add(archive, ref); err != nil {
		t.Fatalf("seed the charly image store with %s: %v", ref, err)
	}
}

// writeNamedExport writes a minimal OCI image export NAMED by `io.containerd.image.name` — the
// shape `buildctl --output type=oci,name=<ref>` writes and every engine measured imports with the
// reference already applied.
func writeNamedExport(t *testing.T, path, ref string) {
	t.Helper()
	config := []byte(`{"architecture":"amd64","os":"linux"}`)
	layer := []byte("deploykit-charly-source-layer")
	configDigest, err := storekit.DigestOf(config)
	if err != nil {
		t.Fatal(err)
	}
	layerDigest, err := storekit.DigestOf(layer)
	if err != nil {
		t.Fatal(err)
	}
	manifest := mustJSON(t, map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    configDigest,
			"size":      len(config),
		},
		"layers": []any{map[string]any{
			"mediaType": "application/vnd.oci.image.layer.v1.tar",
			"digest":    layerDigest,
			"size":      len(layer),
		}},
	})
	manifestDigest, err := storekit.DigestOf(manifest)
	if err != nil {
		t.Fatal(err)
	}
	index := mustJSON(t, map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests": []any{map[string]any{
			"mediaType": "application/vnd.oci.image.manifest.v1+json",
			"digest":    manifestDigest,
			"size":      len(manifest),
			"annotations": map[string]string{
				storekit.AnnotationImageName: ref,
			},
		}},
	})

	f, err := os.Create(path) // #nosec G304 — a test path this file chose.
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	members := map[string][]byte{
		"oci-layout":                   []byte(`{"imageLayoutVersion":"1.0.0"}`),
		"index.json":                   index,
		blobMemberName(manifestDigest): manifest,
		blobMemberName(configDigest):   config,
		blobMemberName(layerDigest):    layer,
	}
	for name, data := range members {
		hdr := &tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// blobMemberName renders the archive member name of a content address.
func blobMemberName(digest string) string {
	return "blobs/" + strings.Replace(digest, ":", "/", 1)
}

// TestCharlyImageSourceIsTheStoreAndTheResolverImportsFromIt pins the adapter's answers AND the
// resolution that consumes them: Has/List are the store's, the tag alone is not a key, and the
// resolve imports the materialised archive into the target store.
func TestCharlyImageSourceIsTheStoreAndTheResolverImportsFromIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv(storekit.ImageStoreDirEnv, dir)
	const ref = "ghcr.io/opencharly/box:2026.296.1200"
	seedImageStore(t, dir, ref)

	source, err := NewCharlyImageSource()
	if err != nil {
		t.Fatalf("NewCharlyImageSource: %v", err)
	}

	for _, probe := range []struct {
		ref  string
		want bool
	}{
		{ref, true},
		{"ghcr.io/opencharly/box:2026.296.1299", false},
		// The TAG ALONE: what `org.opencontainers.image.ref.name` carries. It is not a key.
		{"2026.296.1200", false},
	} {
		got, err := source.Has(ctx, probe.ref)
		if err != nil {
			t.Fatalf("Has(%q): %v", probe.ref, err)
		}
		if got != probe.want {
			t.Errorf("Has(%q) = %v, want %v", probe.ref, got, probe.want)
		}
	}

	refs, err := source.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 || refs[0] != ref {
		t.Fatalf("List() = %v, want [%q]", refs, ref)
	}

	target := newRecordingStore()
	resolved, err := (&ImageResolver{Charly: source}).ResolveImage(ctx, ref, target)
	if err != nil {
		t.Fatalf("ResolveImage(%q): %v", ref, err)
	}
	if resolved.Origin != OriginImported || resolved.Ref != ref {
		t.Errorf("ResolveImage = (%q, %q), want (%q, %q)", resolved.Ref, resolved.Origin, ref, OriginImported)
	}
	if len(target.imported) != 1 {
		t.Fatalf("the target store imported %d archives, want exactly 1", len(target.imported))
	}
	// THE ARCHIVE BELONGS TO THE CALLER, and the resolver is the caller: a successful resolve must
	// not leave a copy of the image on disk.
	if _, err := os.Stat(target.imported[0]); !os.IsNotExist(err) {
		t.Errorf("the resolve left the materialised archive %s behind (stat error = %v)", target.imported[0], err)
	}
}

// TestCharlyImageSourceArchiveCarriesTheNamingAnnotation reads the archive the source materialises
// BACK as a tar: the annotation an engine applies on load must be in its index, and the layout must
// be complete.
func TestCharlyImageSourceArchiveCarriesTheNamingAnnotation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv(storekit.ImageStoreDirEnv, dir)
	const ref = "ghcr.io/opencharly/box:2026.296.1200"
	seedImageStore(t, dir, ref)

	source, err := NewCharlyImageSource()
	if err != nil {
		t.Fatalf("NewCharlyImageSource: %v", err)
	}
	path, err := source.Archive(ctx, ref)
	if err != nil {
		t.Fatalf("Archive(%q): %v", ref, err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	raw, err := os.ReadFile(path) // #nosec G304 — the path Archive just returned.
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}
	var indexJSON []byte
	blobs := 0
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, rerr := tr.Next()
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			t.Fatalf("read the archive: %v", rerr)
		}
		switch {
		case hdr.Name == "index.json":
			if indexJSON, rerr = io.ReadAll(tr); rerr != nil {
				t.Fatalf("read %s: %v", hdr.Name, rerr)
			}
		case hdr.Name == "oci-layout":
			// present is all this arm needs: an OCI archive without it is not a layout.
		case len(hdr.Name) > len("blobs/") && hdr.Name[:len("blobs/")] == "blobs/":
			blobs++
		}
	}
	if indexJSON == nil {
		t.Fatal("the archive carries no index.json")
	}
	var index struct {
		Manifests []struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(indexJSON, &index); err != nil {
		t.Fatalf("parse the archive's index.json: %v", err)
	}
	if len(index.Manifests) != 1 {
		t.Fatalf("the archive's index carries %d manifests, want 1", len(index.Manifests))
	}
	if got := index.Manifests[0].Annotations[storekit.AnnotationImageName]; got != ref {
		t.Errorf("the archive names %q, want the full reference %q — an engine load would land it untagged", got, ref)
	}
	if blobs != 3 {
		t.Errorf("the archive carries %d blobs, want the manifest, the config and the layer", blobs)
	}
}

// TestCharlyImageSourceOnAnAbsentStoreAnswersEmpty: a host that has never built an image has an
// EMPTY store, not a broken one — the resolver must reach its registry/build steps rather than fail.
func TestCharlyImageSourceOnAnAbsentStoreAnswersEmpty(t *testing.T) {
	ctx := context.Background()
	t.Setenv(storekit.ImageStoreDirEnv, filepath.Join(t.TempDir(), "never-written"))
	source, err := NewCharlyImageSource()
	if err != nil {
		t.Fatalf("NewCharlyImageSource: %v", err)
	}
	if held, err := source.Has(ctx, "ghcr.io/opencharly/box:2026.296.1200"); err != nil || held {
		t.Errorf("Has on an absent store = (%v, %v), want (false, nil)", held, err)
	}
	refs, err := source.List(ctx)
	if err != nil {
		t.Fatalf("List on an absent store: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("List on an absent store = %v, want empty", refs)
	}
	if _, err := source.Archive(ctx, "ghcr.io/opencharly/box:2026.296.1200"); err == nil {
		t.Error("Archive on an absent store succeeded, but the store holds no such image")
	}

	// A cancelled context is reported, not ignored: the resolver's caller may have given up before
	// the store was asked anything.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := source.Has(cancelled, "ghcr.io/opencharly/box:2026.296.1200"); err == nil {
		t.Error("Has on a cancelled context returned no error")
	}
}
