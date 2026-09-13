package packagekit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goreleaser/nfpm/v2/files"
)

func writeBin(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestBuildContentsSharedHost verifies the shared-host packaging layout
// (opencharly/sdk#256): the `charly-lib` host ships once, each plugin is a
// symlink to it, and the providers manifest rides along.
func TestBuildContentsSharedHost(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "charly")
	writeBin(t, binary)
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBin(t, filepath.Join(pluginsDir, "charly-lib"))
	if err := os.Symlink("charly-lib", filepath.Join(pluginsDir, "plugin-clean")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginsDir, "plugin-clean.providers"), []byte("command:clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	contents, err := buildContents(binary, pluginsDir, []string{"plugin-clean"})
	if err != nil {
		t.Fatalf("buildContents: %v", err)
	}
	byDest := map[string]*files.Content{}
	for _, c := range contents {
		byDest[c.Destination] = c
	}
	if c := byDest["/usr/lib/charly/plugins/charly-lib"]; c == nil || c.Type == files.TypeSymlink {
		t.Errorf("shared host staged wrong (want a real file): %+v", c)
	}
	if c := byDest["/usr/lib/charly/plugins/plugin-clean"]; c == nil {
		t.Error("plugin symlink not staged")
	} else if c.Type != files.TypeSymlink || c.Source != "charly-lib" {
		t.Errorf("plugin must ship as a symlink to charly-lib: type=%q source=%q", c.Type, c.Source)
	}
	if byDest["/usr/lib/charly/plugins/plugin-clean.providers"] == nil {
		t.Error("providers manifest not staged")
	}
}

// TestBuildContentsRejectsPlainPlugin — the per-plugin binary layout is retired;
// a regular-file plugin entry is a hard error, never silently shipped.
func TestBuildContentsRejectsPlainPlugin(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "charly")
	writeBin(t, binary)
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBin(t, filepath.Join(pluginsDir, "charly-lib"))
	writeBin(t, filepath.Join(pluginsDir, "plugin-doctor")) // a real binary, not a symlink

	_, err := buildContents(binary, pluginsDir, []string{"plugin-doctor"})
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("buildContents(plain plugin) = %v, want a regular-file error", err)
	}
}

// TestBuildContentsRequiresHost — a plugin symlink with no host is a dangling
// link; fail loudly.
func TestBuildContentsRequiresHost(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "charly")
	writeBin(t, binary)
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("charly-lib", filepath.Join(pluginsDir, "plugin-clean")); err != nil {
		t.Fatal(err)
	}
	_, err := buildContents(binary, pluginsDir, []string{"plugin-clean"})
	if err == nil || !strings.Contains(err.Error(), "shared host") {
		t.Fatalf("buildContents(no host) = %v, want a shared-host error", err)
	}
}
