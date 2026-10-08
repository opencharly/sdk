package loaderkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An EMPTY, DECLARED submodule directory must be NAMED, not reported as a missing file.
//
// Measured on opencharly/charly#847: in a fresh `git worktree` every submodule directory exists but
// holds nothing, so the namespaced import of `charly` resolved to <root>/charly, the IsDir branch
// appended the unified file name, and the read returned a bare ENOENT. The same tree on an older
// engine surfaced it as `json: unsupported value: encountered a cycle via map[string]*spec.UnifiedFile`.
// Both readings sent the reader into the loader; neither named the cause.
func TestUninitializedSubmoduleIsNamed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitmodules"),
		[]byte("[submodule \"charly\"]\n\tpath = charly\n\turl = https://github.com/opencharly/charly\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The gitlink directory EXISTS but is empty - exactly what a fresh worktree looks like.
	dir := filepath.Join(root, "charly")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "charly.yml")

	w := &walker{}
	err := w.uninitializedSubmodule(target)
	if err == nil {
		t.Fatalf("empty declared submodule %s reported as nil; want a named error", dir)
	}
	msg := err.Error()
	for _, want := range []string{`submodule "charly"`, "is empty", "git submodule update --init charly"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not carry %q", msg, want)
		}
	}
}

// A directory WITH content is left alone: the ordinary missing-file path must keep working for a
// genuinely wrong ref, so the hint must never fire on a populated tree.
func TestUninitializedSubmoduleIgnoresPopulatedDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitmodules"), []byte("[submodule \"charly\"]\n\tpath = charly\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "charly")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.yml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&walker{}).uninitializedSubmodule(filepath.Join(dir, "charly.yml")); err != nil {
		t.Errorf("populated directory produced %v; want nil", err)
	}
}

// An empty directory that NO .gitmodules declares is not a submodule: say nothing rather than
// name a submodule that does not exist.
func TestUninitializedSubmoduleRequiresDeclaration(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "notasubmodule")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&walker{}).uninitializedSubmodule(filepath.Join(dir, "charly.yml")); err != nil {
		t.Errorf("undeclared empty directory produced %v; want nil", err)
	}
}

// The target already existing means there is nothing to explain.
func TestUninitializedSubmoduleIgnoresExistingTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "charly")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "charly.yml")
	if err := os.WriteFile(target, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&walker{}).uninitializedSubmodule(target); err != nil {
		t.Errorf("existing target produced %v; want nil", err)
	}
}
