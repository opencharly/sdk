package buildkit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectRemoteBuildRef_ExplicitRemoteRef: a box arg that is itself a remote @ref pivots
// immediately, independent of any workspace charly.yml.
func TestDetectRemoteBuildRef_ExplicitRemoteRef(t *testing.T) {
	ref, ok := DetectRemoteBuildRef(t.TempDir(), []string{"@github.com/opencharly/charly/fedora:v1.0.0"})
	if !ok {
		t.Fatalf("explicit remote ref: want pivot, got none")
	}
	if ref != "@github.com/opencharly/charly/fedora:v1.0.0" {
		t.Errorf("ref = %q, want the input ref verbatim", ref)
	}
}

// TestDetectRemoteBuildRef_NoPivot: a local box with no workspace charly.yml (or a plain local
// project) builds locally — no pivot.
func TestDetectRemoteBuildRef_NoPivot(t *testing.T) {
	if ref, ok := DetectRemoteBuildRef(t.TempDir(), []string{"fedora"}); ok {
		t.Errorf("local box in an empty dir: want no pivot, got %q", ref)
	}
}

// TestDetectRemoteBuildRef_Passthrough: a thin workspace whose SOLE import is one flat remote ref
// auto-pivots a locally-undeclared image to `@<repo-root>/<image>:<ref>`.
func TestDetectRemoteBuildRef_Passthrough(t *testing.T) {
	dir := t.TempDir()
	yml := "import:\n  - '@github.com/opencharly/charly/charly.yml:v2026.1.0'\n"
	if err := os.WriteFile(filepath.Join(dir, "charly.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, ok := DetectRemoteBuildRef(dir, []string{"versa"})
	if !ok {
		t.Fatalf("thin passthrough workspace: want pivot, got none")
	}
	if want := "@github.com/opencharly/charly/versa:v2026.1.0"; ref != want {
		t.Errorf("ref = %q, want %q", ref, want)
	}
}

// TestDetectRemoteBuildRef_PassthroughSkippedWhenLocal: the passthrough does NOT fire when the
// requested image is declared locally in the workspace charly.yml.
func TestDetectRemoteBuildRef_PassthroughSkippedWhenLocal(t *testing.T) {
	dir := t.TempDir()
	yml := "import:\n  - '@github.com/opencharly/charly/charly.yml:v2026.1.0'\nbox:\n  versa: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "charly.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	if ref, ok := DetectRemoteBuildRef(dir, []string{"versa"}); ok {
		t.Errorf("locally-declared image: want no pivot, got %q", ref)
	}
}

// TestDetectRemoteBuildRef_PassthroughSkippedWhenMultipleImports: the passthrough is conservative —
// it only fires for a SOLE flat import, never a project with several imports.
func TestDetectRemoteBuildRef_PassthroughSkippedWhenMultipleImports(t *testing.T) {
	dir := t.TempDir()
	yml := "import:\n  - '@github.com/opencharly/charly/charly.yml:v2026.1.0'\n  - '@github.com/opencharly/extra/charly.yml:v1'\n"
	if err := os.WriteFile(filepath.Join(dir, "charly.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	if ref, ok := DetectRemoteBuildRef(dir, []string{"versa"}); ok {
		t.Errorf("multiple imports: want no pivot, got %q", ref)
	}
}
