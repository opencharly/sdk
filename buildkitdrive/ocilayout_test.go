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

// TestBuildArgsRendersOCILayout is the flag the drive actually needs: buildctl parses
// `--oci-layout id=path` into SolveOpt.OCIStores itself, so rendering it correctly is the
// whole feature — there is no session code to write on this side.
func TestBuildArgsRendersOCILayout(t *testing.T) {
	dir := writeOCILayoutDir(t)
	cf := filepath.Join(t.TempDir(), "Containerfile")
	if err := os.WriteFile(cf, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, err := Build{
		Containerfile: cf,
		Context:       t.TempDir(),
		Ref:           "example:v1",
		OCILayouts:    []OCILayout{{ID: "src", Dir: dir}},
	}.Args("/tmp/out.oci.tar")
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	want := false
	for i, a := range args {
		if a == "--oci-layout" && i+1 < len(args) && args[i+1] == "src="+dir {
			want = true
		}
	}
	if !want {
		t.Fatalf("--oci-layout src=%s not rendered; args=%v", dir, args)
	}
}

// TestBuildArgsRejectsANonLayout keeps the bridge.go standard: a layout the solve cannot
// actually read must fail at the boundary, not inside a build with a confusing frontend
// error. A bare directory is exactly what local.NewStore would silently accept.
func TestBuildArgsRejectsANonLayout(t *testing.T) {
	cf := filepath.Join(t.TempDir(), "Containerfile")
	if err := os.WriteFile(cf, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build{
		Containerfile: cf,
		Context:       t.TempDir(),
		Ref:           "example:v1",
		OCILayouts:    []OCILayout{{ID: "src", Dir: t.TempDir()}},
	}.Args("/tmp/out.oci.tar")
	if err == nil {
		t.Fatal("expected an error for a directory that is not an OCI layout")
	}
}

// TestBuildArgsRejectsALayoutWithoutAnID: a solve addresses a layout as oci-layout://<id>,
// so an id-less layout is unaddressable even if the directory is well-formed.
func TestBuildArgsRejectsALayoutWithoutAnID(t *testing.T) {
	cf := filepath.Join(t.TempDir(), "Containerfile")
	if err := os.WriteFile(cf, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build{
		Containerfile: cf,
		Context:       t.TempDir(),
		Ref:           "example:v1",
		OCILayouts:    []OCILayout{{Dir: writeOCILayoutDir(t)}},
	}.Args("/tmp/out.oci.tar")
	if err == nil {
		t.Fatal("expected an error for a layout with no id")
	}
}
