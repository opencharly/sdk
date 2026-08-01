package kit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverRemoteCandies(t *testing.T) {
	dir := t.TempDir()
	candiesDir := filepath.Join(dir, "candy")
	if err := os.MkdirAll(filepath.Join(candiesDir, "beta"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(candiesDir, "alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candiesDir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	names, err := DiscoverRemoteCandy(dir)
	if err != nil {
		t.Fatalf("DiscoverRemoteCandy() error = %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("len(names) = %d, want 2", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("names = %v, want [alpha beta]", names)
	}
}
