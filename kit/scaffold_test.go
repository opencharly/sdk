package kit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldCandy(t *testing.T) {
	tmpDir := t.TempDir()

	if err := ScaffoldCandy(tmpDir, "test-layer", "2026.001.0000"); err != nil {
		t.Fatalf("ScaffoldCandy() error = %v", err)
	}

	candyDir := filepath.Join(tmpDir, DefaultCandyDir, "test-layer")
	if _, err := os.Stat(candyDir); os.IsNotExist(err) {
		t.Error("candy directory was not created")
	}

	candyYml := filepath.Join(candyDir, UnifiedFileName)
	if _, err := os.Stat(candyYml); os.IsNotExist(err) {
		t.Error("candy manifest was not created")
	}
}

func TestScaffoldCandyAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()

	candyDir := filepath.Join(tmpDir, DefaultCandyDir, "existing")
	if err := os.MkdirAll(candyDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ScaffoldCandy(tmpDir, "existing", "2026.001.0000"); err == nil {
		t.Error("expected error for existing candy")
	}
}

func TestScaffoldProject(t *testing.T) {
	dir := t.TempDir()

	if err := ScaffoldProject(dir); err != nil {
		t.Fatalf("ScaffoldProject: %v", err)
	}
	// charly.yml + box/ + candy/ are created.
	for _, want := range []string{UnifiedFileName, DefaultBoxDir, DefaultCandyDir} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("ScaffoldProject did not create %q: %v", want, err)
		}
	}
	// Don't-clobber guard: a second call errors.
	if err := ScaffoldProject(dir); err == nil {
		t.Error("ScaffoldProject on an existing project should error")
	}
}

func TestAddBox(t *testing.T) {
	dir := t.TempDir()
	if err := ScaffoldProject(dir); err != nil {
		t.Fatalf("ScaffoldProject: %v", err)
	}
	if err := AddBox(dir, "hello", "quay.io/fedora/fedora:43", []string{"sshd"}); err != nil {
		t.Fatalf("AddBox: %v", err)
	}
	boxFile := filepath.Join(dir, DefaultBoxDir, "hello", UnifiedFileName)
	if _, err := os.Stat(boxFile); err != nil {
		t.Errorf("AddBox did not create %s: %v", boxFile, err)
	}
	// A second AddBox for the same name errors (don't-clobber).
	if err := AddBox(dir, "hello", "fedora", nil); err == nil {
		t.Error("AddBox on an existing box should error")
	}
}
