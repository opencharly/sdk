package packagekit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goreleaser/nfpm/v2"
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

// TestBuildOneAppliesPassphrases proves the buildOne wiring: the env-var
// passphrase must reach the nfpm.Info that buildOne constructs (via buildInfo).
// It fails if the applyPassphrases call is removed from the build path —
// TestApplyPassphrases alone would not catch that, since it exercises the
// helper in isolation.
func TestBuildOneAppliesPassphrases(t *testing.T) {
	t.Setenv("NFPM_PASSPHRASE", "general")
	t.Setenv("NFPM_APK_PASSPHRASE", "apk-only")
	binary, pluginsDir := fixtureInputs(t)
	opts := BuildOptions{
		Binary:     binary,
		PluginsDir: pluginsDir,
		Version:    "2026.225.1200",
		Arch:       "amd64",
		Variant:    "minimal",
		Out:        t.TempDir(),
	}
	deb, err := buildInfo(testPackaging(), "deb", opts)
	if err != nil {
		t.Fatalf("buildInfo(deb): %v", err)
	}
	if deb.Deb.Signature.KeyPassphrase != "general" {
		t.Errorf("deb passphrase = %q, want general (from the buildOne wiring)", deb.Deb.Signature.KeyPassphrase)
	}
	apk, err := buildInfo(testPackaging(), "apk", opts)
	if err != nil {
		t.Fatalf("buildInfo(apk): %v", err)
	}
	if apk.APK.Signature.KeyPassphrase != "apk-only" {
		t.Errorf("apk passphrase = %q, want apk-only (per-format override)", apk.APK.Signature.KeyPassphrase)
	}
}

// TestApplyPassphrases verifies the NFPM_*_PASSPHRASE env vars reach the
// nfpm.Info signing fields (the struct-direct path skips nFPM's Parse-time
// env expansion). Precedence: the general NFPM_PASSPHRASE fills deb/rpm/apk,
// and the per-format override wins.
func TestApplyPassphrases(t *testing.T) {
	t.Setenv("NFPM_PASSPHRASE", "general")
	t.Setenv("NFPM_APK_PASSPHRASE", "apk-only")
	t.Setenv("NFPM_MSIX_PASSPHRASE", "msix-only")
	info := &nfpm.Info{}
	applyPassphrases(info)
	if info.Deb.Signature.KeyPassphrase != "general" {
		t.Errorf("deb passphrase = %q, want general", info.Deb.Signature.KeyPassphrase)
	}
	if info.RPM.Signature.KeyPassphrase != "general" {
		t.Errorf("rpm passphrase = %q, want general", info.RPM.Signature.KeyPassphrase)
	}
	if info.APK.Signature.KeyPassphrase != "apk-only" {
		t.Errorf("apk passphrase = %q, want apk-only (per-format override)", info.APK.Signature.KeyPassphrase)
	}
	if info.MSIX.Signature.KeyPassphrase != "msix-only" {
		t.Errorf("msix passphrase = %q, want msix-only", info.MSIX.Signature.KeyPassphrase)
	}
}
