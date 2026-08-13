package packagekit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goreleaser/nfpm/v2"
)

// fixtureInputs writes a fake binary + plugins dir and returns the paths.
func fixtureInputs(t *testing.T) (binary, pluginsDir string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "charly")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho charly\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginsDir = filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"plugin-doctor", "plugin-clean", "plugin-secrets"} {
		if err := os.WriteFile(filepath.Join(pluginsDir, p), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pluginsDir, p+".providers"), []byte(p+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return binary, pluginsDir
}

func TestBuildInfo(t *testing.T) {
	binary, pluginsDir := fixtureInputs(t)
	pkg := testPackaging()
	opts := BuildOptions{
		Binary:     binary,
		PluginsDir: pluginsDir,
		Version:    "2026.225.1200",
		Arch:       "amd64",
		Variant:    "minimal",
	}

	info, err := BuildInfo(pkg, "deb", opts)
	if err != nil {
		t.Fatalf("BuildInfo: %v", err)
	}
	if info.Name != "charly-minimal" {
		t.Errorf("Name = %q, want charly-minimal", info.Name)
	}
	if info.Arch != "amd64" {
		t.Errorf("Arch = %q, want amd64", info.Arch)
	}
	if info.Version != "2026.225.1200" {
		t.Errorf("Version = %q", info.Version)
	}
	if len(info.Contents) != 3 { // binary + 1 plugin + 1 .providers
		t.Errorf("Contents = %d entries, want 3", len(info.Contents))
	}
	if info.Contents[0].Destination != "/usr/bin/charly" {
		t.Errorf("binary destination = %q", info.Contents[0].Destination)
	}

	// archlinux: arch mapped + optdepends carried on the format (injected post-build).
	info, err = BuildInfo(pkg, "archlinux", opts)
	if err != nil {
		t.Fatalf("BuildInfo archlinux: %v", err)
	}
	if info.ArchLinux.Arch != "x86_64" {
		t.Errorf("ArchLinux.Arch = %q, want x86_64", info.ArchLinux.Arch)
	}
}

func TestBuildInfoMissingPlugin(t *testing.T) {
	binary, pluginsDir := fixtureInputs(t)
	pkg := testPackaging()
	opts := BuildOptions{
		Binary:     binary,
		PluginsDir: pluginsDir,
		Version:    "2026.225.1200",
		Arch:       "amd64",
		Variant:    "broken", // lists plugin-nope, absent from the fixture plugins dir
	}
	if _, err := BuildInfo(pkg, "deb", opts); err == nil {
		t.Fatal("expected error for variant plugin missing from the plugins dir")
	}
}

func TestBuildInfoValidate(t *testing.T) {
	binary, pluginsDir := fixtureInputs(t)
	pkg := testPackaging()
	opts := BuildOptions{
		Binary:     binary,
		PluginsDir: pluginsDir,
		Version:    "2026.225.1200",
		Arch:       "amd64",
	}
	info, err := BuildInfo(pkg, "deb", opts)
	if err != nil {
		t.Fatalf("BuildInfo: %v", err)
	}
	info = nfpm.WithDefaults(info)
	if err := nfpm.Validate(info); err != nil {
		t.Fatalf("nfpm.Validate: %v", err)
	}
}
