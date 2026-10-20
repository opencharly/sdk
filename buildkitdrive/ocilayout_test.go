package buildkitdrive

import (
	"os"
	"path/filepath"
	"testing"
)

// writeOCILayoutDir creates the skeleton of an OCI image layout: the `oci-layout`
// marker, an `index.json`, and an (empty) `blobs/sha256/` tree. The blobs tree is
// deliberately empty — a layout's structure is the marker plus the index; blobs are
// content, and a layout with none is valid, just empty.
//
// Note that local.NewStore does NOT validate this: given an empty directory it
// initialises a fresh, empty layout. The structure below is what `Store()` checks
// for, which is why the rejection test passes a bare temp dir instead.
func writeOCILayoutDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "blobs", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(`{"schemaVersion":2,"manifests":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestParseOCILayoutSpec(t *testing.T) {
	got, err := ParseOCILayoutSpec("src=/tmp/layout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "src" || got.Dir != "/tmp/layout" {
		t.Fatalf("got %+v", got)
	}
	for _, bad := range []string{"", "noequals", "=dir", "id="} {
		if _, err := ParseOCILayoutSpec(bad); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}

func TestStoreOpensARealLayout(t *testing.T) {
	dir := writeOCILayoutDir(t)
	cs, err := OCILayout{ID: "ctx", Dir: dir}.Store()
	if err != nil {
		t.Fatalf("Store on a well-formed layout: %v", err)
	}
	if cs == nil {
		t.Fatal("nil store")
	}
}

func TestStoreRejectsANonLayout(t *testing.T) {
	if _, err := (OCILayout{ID: "ctx", Dir: t.TempDir()}).Store(); err == nil {
		t.Fatal("expected an error opening a directory that is not an OCI layout")
	}
}

func TestContentStoresRejectsDuplicateIDs(t *testing.T) {
	a, b := writeOCILayoutDir(t), writeOCILayoutDir(t)
	_, err := ContentStores(OCILayout{ID: "same", Dir: a}, OCILayout{ID: "same", Dir: b})
	if err == nil {
		t.Fatal("expected a duplicate-id error")
	}
	if _, err := ContentStores(OCILayout{ID: "a", Dir: a}, OCILayout{ID: "b", Dir: b}); err != nil {
		t.Fatalf("distinct ids: %v", err)
	}
}
