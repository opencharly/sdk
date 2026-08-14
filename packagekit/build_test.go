package packagekit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuild exercises the full nFPM build for every registered format from a
// fixture packaging section + binary + plugins dir, and asserts the written
// package structure (contents + deps + optdepends + variant plugin sets).
func TestBuild(t *testing.T) {
	binary, pluginsDir := fixtureInputs(t)
	pkg := testPackaging()
	out := t.TempDir()
	opts := BuildOptions{
		Binary:     binary,
		PluginsDir: pluginsDir,
		Version:    "2026.225.1200",
		Arch:       "amd64",
		Variant:    "minimal",
		Formats:    []string{"deb", "rpm", "apk", "archlinux", "ipk"},
		Out:        out,
	}
	written, err := Build(pkg, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(written) != 5 {
		t.Fatalf("written = %d packages, want 5", len(written))
	}
	for _, path := range written {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("written package %s: %v", path, err)
		}
	}
	// The archlinux package must carry the injected optdepends.
	var archPkg string
	for _, path := range written {
		if strings.HasSuffix(path, ".pkg.tar.zst") {
			archPkg = path
		}
	}
	if archPkg == "" {
		t.Fatal("no archlinux package written")
	}
	got := readPkgInfo(t, archPkg)
	if !strings.Contains(got, "optdepend = qemu-full: full-system VM support") {
		t.Errorf("archlinux package missing injected optdepends:\n%s", got)
	}
}

// TestBuildDefaultVariant builds the default variant (plain `charly` name).
func TestBuildDefaultVariant(t *testing.T) {
	binary, pluginsDir := fixtureInputs(t)
	pkg := testPackaging()
	out := t.TempDir()
	opts := BuildOptions{
		Binary:     binary,
		PluginsDir: pluginsDir,
		Version:    "2026.225.1200",
		Arch:       "amd64",
		Formats:    []string{"deb"},
		Out:        out,
	}
	written, err := Build(pkg, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("written = %d, want 1", len(written))
	}
	if !strings.Contains(filepath.Base(written[0]), "charly_2026.225.1200") {
		t.Errorf("default-variant package name = %s, want charly_2026.225.1200", filepath.Base(written[0]))
	}
}
