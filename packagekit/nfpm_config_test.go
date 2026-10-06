package packagekit

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/goreleaser/nfpm/v2"
)

// fixtureInputs writes a fake binary + a shared-host plugins dir (one
// charly-lib + plugin-<word> symlinks to it) and returns the paths.
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
	if err := os.WriteFile(filepath.Join(pluginsDir, "charly-lib"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"plugin-doctor", "plugin-clean", "plugin-secrets"} {
		if err := os.Symlink("charly-lib", filepath.Join(pluginsDir, p)); err != nil {
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
	if len(info.Contents) != 4 { // binary + charly-lib host + 1 plugin symlink + 1 .providers
		t.Errorf("Contents = %d entries, want 4", len(info.Contents))
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

func TestBuildInfoVariantRelationships(t *testing.T) {
	binary, pluginsDir := fixtureInputs(t)
	pkg := testPackaging()
	// The shared fixture carries a deliberate "broken" variant (a plugin absent
	// from the plugins dir) exercised by TestBuildInfoMissingPlugin; drop it so
	// every family member here actually builds.
	delete(pkg.Variants, "broken")

	// The variant family is mutually-exclusive over a shared file set, so each
	// package must Provides the base name and Conflicts+Replaces every OTHER
	// family member — otherwise `pacman -S charly-minimal` over `charly` aborts on
	// the shared /usr/bin/charly + plugin paths (the defect this covers). The
	// family is format-derived, so assert the contract against it directly.
	for _, format := range []string{"archlinux", "deb", "rpm", "apk", "ipk"} {
		family := FamilyNames(pkg, format)
		for _, variant := range VariantNames(pkg, format) {
			info, err := BuildInfo(pkg, format, BuildOptions{
				Binary: binary, PluginsDir: pluginsDir, Version: "2026.225.1200",
				Arch: "amd64", Variant: variant,
			})
			if err != nil {
				t.Fatalf("BuildInfo(%s, %q): %v", format, variant, err)
			}
			if !reflect.DeepEqual(info.Provides, []string{"charly"}) {
				t.Errorf("%s/%s Provides = %v, want [charly]", format, variant, info.Provides)
			}
			var want []string
			for _, fam := range family {
				if fam != info.Name {
					want = append(want, fam)
				}
			}
			if !reflect.DeepEqual(info.Conflicts, want) {
				t.Errorf("%s/%s Conflicts = %v, want %v", format, variant, info.Conflicts, want)
			}
			if !reflect.DeepEqual(info.Replaces, want) {
				t.Errorf("%s/%s Replaces = %v, want %v", format, variant, info.Replaces, want)
			}
			for _, n := range append(append([]string{}, info.Conflicts...), info.Replaces...) {
				if n == info.Name {
					t.Errorf("%s/%s lists ITSELF (%q) in conflicts/replaces", format, variant, n)
				}
			}
		}
	}

	// The deb family specifically: default_variant "default" → plain `charly`.
	info, err := BuildInfo(pkg, "deb", BuildOptions{
		Binary: binary, PluginsDir: pluginsDir, Version: "2026.225.1200",
		Arch: "amd64", Variant: "minimal",
	})
	if err != nil {
		t.Fatalf("BuildInfo(deb, minimal): %v", err)
	}
	if info.Name != "charly-minimal" {
		t.Fatalf("Name = %q, want charly-minimal", info.Name)
	}
	want := []string{"charly", "charly-full"}
	if !reflect.DeepEqual(info.Conflicts, want) {
		t.Errorf("deb/minimal Conflicts = %v, want %v", info.Conflicts, want)
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
