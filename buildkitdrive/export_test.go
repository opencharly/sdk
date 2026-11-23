package buildkitdrive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemoveExport_RemovesTheArchive pins the success path.
func TestRemoveExport_RemovesTheArchive(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "img.oci.tar")
	if err := os.WriteFile(dest, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveExport(dest); err != nil {
		t.Fatalf("RemoveExport: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("the archive survived: %v", err)
	}
}

// TestRemoveExport_MissingIsNotAnError is the failure-path contract: a build that never
// created the archive (it failed before the exporter opened it) must not gain a second
// failure from the cleanup.
func TestRemoveExport_MissingIsNotAnError(t *testing.T) {
	if err := RemoveExport(filepath.Join(t.TempDir(), "never-written.oci.tar")); err != nil {
		t.Fatalf("a missing archive must be a no-op, got %v", err)
	}
	if err := RemoveExport(""); err != nil {
		t.Fatalf("an empty destination must be a no-op, got %v", err)
	}
}

// TestRemoveExport_ReportsARealFailure: the rule is "remove on every exit path", not "hide a
// removal error" — a path that cannot be removed is named.
func TestRemoveExport_ReportsARealFailure(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory is not an archive: os.Remove refuses it, and the refusal is named.
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := RemoveExport(dir)
	if err == nil {
		t.Fatal("removing a non-empty directory must fail")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("the error must name the path %q: %v", dir, err)
	}
}
