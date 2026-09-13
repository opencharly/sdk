package packagekit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goreleaser/nfpm/v2/files"
)

// TestBuildContentsSharedHost verifies the shared-plugin-host packaging shape
// (opencharly/sdk#256): a `charly-lib` host ships once, plugin entries that are
// symlinks ship as symlinks to it, and a plain file entry still ships as a
// real binary (the legacy layout).
func TestBuildContentsSharedHost(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "charly")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The shared host + a plugin symlinked to it + a providers manifest.
	if err := os.WriteFile(filepath.Join(pluginsDir, "charly-lib"), []byte("host"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("charly-lib", filepath.Join(pluginsDir, "plugin-clean")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginsDir, "plugin-clean.providers"), []byte("command:clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A legacy plain (non-symlink) plugin entry.
	if err := os.WriteFile(filepath.Join(pluginsDir, "plugin-legacy"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	contents, err := buildContents(binary, pluginsDir, []string{"plugin-clean", "plugin-legacy"})
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
		t.Error("symlinked plugin not staged")
	} else if c.Type != files.TypeSymlink || c.Source != "charly-lib" {
		t.Errorf("symlinked plugin must ship as a symlink to charly-lib: type=%q source=%q", c.Type, c.Source)
	}
	if c := byDest["/usr/lib/charly/plugins/plugin-legacy"]; c == nil || c.Type == files.TypeSymlink {
		t.Errorf("plain plugin must ship as a real file: %+v", c)
	}
	if byDest["/usr/lib/charly/plugins/plugin-clean.providers"] == nil {
		t.Error("providers manifest not staged")
	}
}

// TestBuildContentsNoHost is the legacy layout: no charly-lib, no symlinks.
func TestBuildContentsNoHost(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "charly")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginsDir, "plugin-doctor"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	contents, err := buildContents(binary, pluginsDir, []string{"plugin-doctor"})
	if err != nil {
		t.Fatalf("buildContents: %v", err)
	}
	for _, c := range contents {
		if c.Destination == "/usr/lib/charly/plugins/charly-lib" {
			t.Error("no host should be staged when charly-lib is absent")
		}
		if c.Destination == "/usr/lib/charly/plugins/plugin-doctor" && c.Type == files.TypeSymlink {
			t.Error("plain plugin must not be a symlink")
		}
	}
}
