package deploykit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

// Tests for the localpkg subsystem relocated from charly core (W3): ResolveLocalPkgDir,
// BuildLocalPkgOnHost, TransferAndInstallPkgs, VenueHasPkgManager, ExecLocalPkgInstall,
// RenderLocalPkgImageInstall. Every test here is PURE — no *Config, no live *Candy graph, no
// provider registry — matching the source file's own design (sdk/deploykit/localpkg.go).

// testPacLocalPkgDef returns a LocalPkgDef mirroring charly.yml's `pac.local_pkg`
// block — the config that drives the localpkg mechanism. Tests use it so they
// exercise the SAME config-driven path the loader produces, without parsing YAML.
func testPacLocalPkgDef() *LocalPkgDef {
	return &LocalPkgDef{
		PkgGlob:         "*.pkg.tar.zst",
		SourceSentinel:  "PKGBUILD",
		BuildTemplate:   "cd {{.SrcDir}} && PKGDEST={{.PkgDest}} makepkg -sf --noconfirm",
		InstallTemplate: "pacman -U --noconfirm {{.StageDir}}/{{.Glob}}",
		Probe:           "command -v pacman",
		DepBuilder:      "aur",
	}
}

// TestResolveLocalPkgDir covers source-dir resolution across the four branches
// (absolute, candy-relative, project-relative, walk-up) AND the config-driven
// per-format sentinel: PKGBUILD (plain file), *.spec (glob), debian/control
// (sub-path). A missing sentinel returns "".
func TestResolveLocalPkgDir(t *testing.T) {
	root := t.TempDir()
	// <root>/pkg/arch/PKGBUILD (superproject) and a nested project dir.
	pkgArch := filepath.Join(root, "pkg", "arch")
	if err := os.MkdirAll(pkgArch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgArch, "PKGBUILD"), []byte("pkgname=opencharly-git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nestedProject := filepath.Join(root, "image", "cachyos")
	if err := os.MkdirAll(nestedProject, 0o755); err != nil {
		t.Fatal(err)
	}
	// A candy dir that bundles its OWN PKGBUILD (candy-relative branch).
	candyWithPkg := filepath.Join(root, "candy", "mytool")
	if err := os.MkdirAll(filepath.Join(candyWithPkg, "arch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candyWithPkg, "arch", "PKGBUILD"), []byte("pkgname=mytool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// rpm source dir (sentinel is a *.spec glob) and deb source dir (sentinel is
	// a debian/control sub-path) — proving the generic sentinel match.
	pkgFedora := filepath.Join(root, "pkg", "fedora")
	if err := os.MkdirAll(pkgFedora, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgFedora, "opencharly.spec"), []byte("Name: opencharly\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDebian := filepath.Join(root, "pkg", "debian", "debian")
	if err := os.MkdirAll(pkgDebian, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDebian, "control"), []byte("Source: opencharly\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Absolute ref (PKGBUILD sentinel).
	if got := ResolveLocalPkgDir(pkgArch, "", "", "PKGBUILD"); got != pkgArch {
		t.Errorf("absolute ref = %q, want %q", got, pkgArch)
	}
	// 2. Candy-relative.
	if got := ResolveLocalPkgDir("arch", candyWithPkg, root, "PKGBUILD"); got != filepath.Join(candyWithPkg, "arch") {
		t.Errorf("candy-relative = %q, want %q", got, filepath.Join(candyWithPkg, "arch"))
	}
	// 3. Project-relative (project dir == superproject root).
	if got := ResolveLocalPkgDir("pkg/arch", "/no/such/layer", root, "PKGBUILD"); got != pkgArch {
		t.Errorf("project-relative = %q, want %q", got, pkgArch)
	}
	// 4. Walk-up: project dir is the nested box/cachyos; pkg/arch is two levels up.
	if got := ResolveLocalPkgDir("pkg/arch", "/no/such/layer", nestedProject, "PKGBUILD"); got != pkgArch {
		t.Errorf("walk-up = %q, want %q (must find the superproject pkg/arch from a nested project dir)", got, pkgArch)
	}
	// 5. rpm glob sentinel (*.spec).
	if got := ResolveLocalPkgDir("pkg/fedora", "/no/such/layer", root, "*.spec"); got != pkgFedora {
		t.Errorf("rpm *.spec sentinel = %q, want %q", got, pkgFedora)
	}
	// 6. deb sub-path sentinel (debian/control).
	wantDeb := filepath.Join(root, "pkg", "debian")
	if got := ResolveLocalPkgDir("pkg/debian", "/no/such/layer", root, "debian/control"); got != wantDeb {
		t.Errorf("deb debian/control sentinel = %q, want %q", got, wantDeb)
	}
	// 7. Missing sentinel → "".
	if got := ResolveLocalPkgDir("does/not/exist", "/no/such/layer", nestedProject, "PKGBUILD"); got != "" {
		t.Errorf("missing sentinel = %q, want empty (no-op fallback)", got)
	}
	// 8. Empty ref → "".
	if got := ResolveLocalPkgDir("", candyWithPkg, root, "PKGBUILD"); got != "" {
		t.Errorf("empty ref = %q, want empty", got)
	}
	// 9. Empty sentinel → "" (never matches).
	if got := ResolveLocalPkgDir("pkg/arch", "", root, ""); got != "" {
		t.Errorf("empty sentinel = %q, want empty", got)
	}
}

// localPkgRecExec records RunSystem scripts + PutFile dests so the install-body
// tests can assert the transfer+install leg without a real venue.
type localPkgRecExec struct {
	systemScripts []string
	userScripts   []string
	putDests      []string
	probeYes      bool // canned answer for the config-driven package-manager probe
}

func (e *localPkgRecExec) Venue() string { return "localpkg-rec://test" }
func (e *localPkgRecExec) RunSystem(_ context.Context, script string, _ EmitOpts) error {
	e.systemScripts = append(e.systemScripts, script)
	return nil
}
func (e *localPkgRecExec) RunUser(_ context.Context, script string, _ EmitOpts) error {
	e.userScripts = append(e.userScripts, script)
	return nil
}
func (e *localPkgRecExec) RunBuilder(context.Context, BuilderRunOpts) ([]byte, error) {
	return nil, nil
}
func (e *localPkgRecExec) PutFile(_ context.Context, _, remotePath string, _ uint32, _ bool, _ EmitOpts) error {
	e.putDests = append(e.putDests, remotePath)
	return nil
}
func (e *localPkgRecExec) GetFile(context.Context, string, bool, EmitOpts) ([]byte, error) {
	return nil, nil
}
func (e *localPkgRecExec) RunInteractive(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (e *localPkgRecExec) RunStream(context.Context, string) (int, error) {
	return -1, spec.ErrNotSupported
}
func (e *localPkgRecExec) RunCapture(_ context.Context, _ string) (string, string, int, error) {
	// The probe script echoes "yes"/"no"; mirror that contract.
	if e.probeYes {
		return "yes", "", 0, nil
	}
	return "no", "", 0, nil
}
func (e *localPkgRecExec) Kind() string { return "localpkg-rec" }
func (e *localPkgRecExec) ResolveHome(context.Context, string) (string, error) {
	return "/home/guest", nil
}

// TestVenueHasPkgManager confirms the gate runs the format's config-driven probe
// (LocalPkgDef.Probe), treating only an exact "yes" as supported; DryRun assumes
// true; a nil LocalPkgDef gates false (never assume a target can take a package).
func TestVenueHasPkgManager(t *testing.T) {
	lp := testPacLocalPkgDef()
	yes := &localPkgRecExec{probeYes: true}
	if !VenueHasPkgManager(context.Background(), yes, lp, EmitOpts{}) {
		t.Error("venue reporting the package manager present should gate true")
	}
	no := &localPkgRecExec{probeYes: false}
	if VenueHasPkgManager(context.Background(), no, lp, EmitOpts{}) {
		t.Error("venue without the package manager should gate false")
	}
	// DryRun assumes true regardless of the probe (planner shows what it WOULD do).
	if !VenueHasPkgManager(context.Background(), no, lp, EmitOpts{DryRun: true}) {
		t.Error("DryRun should assume the package manager present")
	}
	// Nil LocalPkgDef → false even on DryRun (no format config = nothing to do).
	if VenueHasPkgManager(context.Background(), yes, nil, EmitOpts{DryRun: true}) {
		t.Error("nil LocalPkgDef should gate false")
	}
}

// TestExecLocalPkgInstall_SkipsUnsupported proves an unsupported venue is a
// clean no-op: no build, no transfer, no install — the candy's curl/COPY task
// installs it instead.
func TestExecLocalPkgInstall_SkipsUnsupported(t *testing.T) {
	exec := &localPkgRecExec{}
	s := &LocalPkgInstallStep{PkgbuildRef: "pkg/arch", CandyName: "charly", ProjectDir: t.TempDir(), Format: "pac", LocalPkg: testPacLocalPkgDef()}
	if err := ExecLocalPkgInstall(context.Background(), exec, s, false /* supported */, "host", EmitOpts{}); err != nil {
		t.Fatalf("unsupported venue should be a clean no-op, got %v", err)
	}
	if len(exec.systemScripts) != 0 || len(exec.putDests) != 0 {
		t.Errorf("unsupported venue must not install anything: systemScripts=%v putDests=%v", exec.systemScripts, exec.putDests)
	}
}

// TestExecLocalPkgInstall_SkipsNilLocalPkg proves a step with no resolved
// LocalPkg config (target distro declares no localpkg-capable format) is a clean
// no-op even when the venue is reported supported.
func TestExecLocalPkgInstall_SkipsNilLocalPkg(t *testing.T) {
	exec := &localPkgRecExec{}
	s := &LocalPkgInstallStep{PkgbuildRef: "pkg/arch", CandyName: "charly", ProjectDir: t.TempDir()} // LocalPkg nil
	if err := ExecLocalPkgInstall(context.Background(), exec, s, true, "host", EmitOpts{}); err != nil {
		t.Fatalf("nil LocalPkg should be a clean no-op, got %v", err)
	}
	if len(exec.systemScripts) != 0 || len(exec.putDests) != 0 {
		t.Errorf("nil LocalPkg must not install anything: systemScripts=%v putDests=%v", exec.systemScripts, exec.putDests)
	}
}

// TestExecLocalPkgInstall_SkipsMissingSource proves a missing source dir on a
// supported venue is ALSO a clean no-op (fallback to the candy's curl/COPY
// task) — not an error that aborts the deploy.
func TestExecLocalPkgInstall_SkipsMissingSource(t *testing.T) {
	exec := &localPkgRecExec{}
	s := &LocalPkgInstallStep{PkgbuildRef: "no/such/source", CandyName: "charly", ProjectDir: t.TempDir(), Format: "pac", LocalPkg: testPacLocalPkgDef()}
	if err := ExecLocalPkgInstall(context.Background(), exec, s, true /* supported */, "host", EmitOpts{}); err != nil {
		t.Fatalf("missing source should be a clean no-op, got %v", err)
	}
	if len(exec.systemScripts) != 0 || len(exec.putDests) != 0 {
		t.Errorf("missing source must not install anything: systemScripts=%v putDests=%v", exec.systemScripts, exec.putDests)
	}
}

// TestTransferAndInstallPkgs proves the shared transfer+install leg stages the
// dir, PutFiles each package, and renders the format's CONFIG-DRIVEN install
// command (LocalPkgDef.InstallTemplate) against the staging glob — venue-agnostic.
func TestTransferAndInstallPkgs(t *testing.T) {
	exec := &localPkgRecExec{}
	lp := testPacLocalPkgDef()
	pkgs := []string{"/tmp/build/opencharly-git-2026.155.0001-1-x86_64.pkg.tar.zst"}
	if err := TransferAndInstallPkgs(context.Background(), exec, lp, pkgs, EmitOpts{}); err != nil {
		t.Fatalf("TransferAndInstallPkgs: %v", err)
	}
	if len(exec.putDests) != 1 || !strings.HasPrefix(exec.putDests[0], localPkgGuestStage) {
		t.Errorf("package not staged under %s: %v", localPkgGuestStage, exec.putDests)
	}
	// The install command is rendered from the config template, not hardcoded.
	wantCmd := "pacman -U --noconfirm " + localPkgGuestStage + "/" + lp.PkgGlob
	if len(exec.systemScripts) != 1 || strings.TrimSpace(exec.systemScripts[0]) != wantCmd {
		t.Errorf("install command = %v, want rendered %q", exec.systemScripts, wantCmd)
	}
	// No packages → error (caller bug, never a silent skip).
	if err := TransferAndInstallPkgs(context.Background(), exec, lp, nil, EmitOpts{}); err == nil {
		t.Error("TransferAndInstallPkgs(nil pkgs) should error")
	}
	// Nil LocalPkgDef → error.
	if err := TransferAndInstallPkgs(context.Background(), exec, nil, pkgs, EmitOpts{}); err == nil {
		t.Error("TransferAndInstallPkgs(nil LocalPkgDef) should error")
	}
}

// TestBuildLocalPkgOnHost_DryRunAndEmpty proves the build leg renders the
// CONFIG-DRIVEN build template (no hardcoded makepkg) and honors DryRun (no
// shell-out), and that a nil/empty config errors rather than silently building.
func TestBuildLocalPkgOnHost_DryRunAndEmpty(t *testing.T) {
	lp := testPacLocalPkgDef()
	// DryRun: renders the template (proving config-driven) but never runs it.
	if pkgs, err := BuildLocalPkgOnHost(context.Background(), lp, "/src/pkg/arch", EmitOpts{DryRun: true}); err != nil || pkgs != nil {
		t.Errorf("dry-run = (%v, %v), want (nil, nil)", pkgs, err)
	}
	// Nil LocalPkgDef → error.
	if _, err := BuildLocalPkgOnHost(context.Background(), nil, "/src", EmitOpts{DryRun: true}); err == nil {
		t.Error("BuildLocalPkgOnHost(nil) should error")
	}
	// Empty build template → error (config missing build_template).
	empty := &LocalPkgDef{PkgGlob: "*.pkg.tar.zst"}
	if _, err := BuildLocalPkgOnHost(context.Background(), empty, "/src", EmitOpts{DryRun: true}); err == nil {
		t.Error("empty build_template should error")
	}
}

func TestBuildLocalPkgOnHostUsesImmutableSourceCopy(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "pkg", "arch")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("pkgver=committed\n")
	if err := os.WriteFile(filepath.Join(srcDir, "PKGBUILD"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "build-helper"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "project-marker"), []byte("project-input\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lp := &LocalPkgDef{
		PkgGlob: "*.pkg",
		BuildTemplate: `set -eu
test -x {{.SrcDir}}/build-helper
test "$(cat {{.SourceDir}}/../../project-marker)" = project-input
printf 'pkgver=mutated\n' > {{.SrcDir}}/PKGBUILD
mkdir -p {{.SrcDir}}/src {{.SrcDir}}/pkg
printf artifact > {{.PkgDest}}/opencharly.pkg`,
	}
	files, err := BuildLocalPkgOnHost(context.Background(), lp, srcDir, EmitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := CleanupBuiltPackageFiles(files); err != nil {
			t.Error(err)
		}
	})
	got, err := os.ReadFile(filepath.Join(srcDir, "PKGBUILD"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("authored PKGBUILD mutated: got %q, want %q", got, original)
	}
	for _, generated := range []string{"src", "pkg"} {
		if _, err := os.Stat(filepath.Join(srcDir, generated)); !os.IsNotExist(err) {
			t.Fatalf("build directory leaked into source at %s: %v", generated, err)
		}
	}
	if len(files) != 1 {
		t.Fatalf("built files = %v, want one artifact", files)
	}
}

func TestBuildLocalPkgOnHostCleansStagingAfterFailure(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	srcDir := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "PKGBUILD"), []byte("unchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lp := &LocalPkgDef{PkgGlob: "*.pkg", BuildTemplate: "printf changed > {{.SrcDir}}/PKGBUILD; exit 19"}
	if _, err := BuildLocalPkgOnHost(context.Background(), lp, srcDir, EmitOpts{}); err == nil {
		t.Fatal("failing build returned nil error")
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed build leaked temporary staging: %v", entries)
	}
	got, err := os.ReadFile(filepath.Join(srcDir, "PKGBUILD"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "unchanged\n" {
		t.Fatalf("failed build mutated authored source: %q", got)
	}
}

func TestStageLocalPkgSourceRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside", filepath.Join(srcDir, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := stageLocalPkgSource(srcDir); err == nil || !strings.Contains(err.Error(), "escapes source") {
		t.Fatalf("escaping symlink error = %v", err)
	}
}

func TestStageLocalPkgSourceExcludesGitIgnoredBuildCaches(t *testing.T) {
	srcDir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = srcDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	for path, content := range map[string]string{
		".gitignore":          "src/\npkg/\nsource-cache/\n",
		"PKGBUILD":            "pkgname=fixture\n",
		"local-input.patch":   "authored untracked input\n",
		"src/stale-worktree":  "must not be staged\n",
		"pkg/stale-artifact":  "must not be staged\n",
		"source-cache/config": "must not be staged\n",
	} {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd = exec.Command("git", "add", ".gitignore", "PKGBUILD")
	cmd.Dir = srcDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}

	stageDir, release, err := stageLocalPkgSource(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	for _, included := range []string{".gitignore", "PKGBUILD", "local-input.patch"} {
		if _, err := os.Stat(filepath.Join(stageDir, included)); err != nil {
			t.Fatalf("authored source %s was not staged: %v", included, err)
		}
	}
	for _, excluded := range []string{".git", "src", "pkg", "source-cache"} {
		if _, err := os.Stat(filepath.Join(stageDir, excluded)); !os.IsNotExist(err) {
			t.Fatalf("ignored build cache %s reached stage: %v", excluded, err)
		}
	}
}

func TestCleanupBuiltPackageFilesIsScopedAndIdempotent(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir, err := os.MkdirTemp("", "charly-localpkg-")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "charly.pkg.tar.zst")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	vmshared.RegisterTempCleanup(dir)
	if err := CleanupBuiltPackageFiles([]string{file}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("artifact directory survived cleanup: %v", err)
	}
	if err := CleanupBuiltPackageFiles([]string{file}); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "important.pkg.tar.zst")
	if err := CleanupBuiltPackageFiles([]string{outside}); err == nil {
		t.Fatal("cleanup accepted a path outside the Charly temp namespaces")
	}
}

// renderLocalPkgImageInstall: a PRODUCTION box build DOWNLOADS the candy's
// PUBLISHED release package (latest released toolchain) and installs it via the
// shared install template — never a COPY of a locally-built package.
func TestRenderLocalPkgImageInstall_ProductionDownloadsRelease(t *testing.T) {
	lp := testPacLocalPkgDef()
	lp.DownloadTemplate = "https://github.com/opencharly/charly/releases/latest/download/opencharly-${ARCH}.pkg.tar.zst"
	s := &LocalPkgInstallStep{CandyName: "charly", Format: "pac", LocalPkg: lp}
	got, err := RenderLocalPkgImageInstall(s, false, "", "")
	if err != nil {
		t.Fatalf("production render: %v", err)
	}
	if !strings.Contains(got, "curl -fsSL") || !strings.Contains(got, "releases/latest/download/opencharly-${ARCH}.pkg.tar.zst") {
		t.Errorf("production mode must DOWNLOAD the published release; got:\n%s", got)
	}
	if !strings.Contains(got, "pacman -U --noconfirm") {
		t.Errorf("production mode must install via the format install_template; got:\n%s", got)
	}
	if strings.Contains(got, "COPY ") {
		t.Errorf("production mode must NOT COPY a locally-built package; got:\n%s", got)
	}
}

// renderLocalPkgImageInstall: a DISPOSABLE check bed (devLocalPkg=true) builds the
// in-development package from LOCAL source. With no localpkg source dir present it
// HARD ERRORS — it must NEVER silently fall back to the published release (R4: no
// black-magic fallback that would let a bed test a stale binary).
func TestRenderLocalPkgImageInstall_DevMissingSourceHardErrors(t *testing.T) {
	lp := testPacLocalPkgDef()
	lp.DownloadTemplate = "https://example.com/opencharly-${ARCH}.pkg.tar.zst"
	s := &LocalPkgInstallStep{
		CandyName:   "charly",
		Format:      "pac",
		PkgbuildRef: "pkg/arch",
		CandyDir:    t.TempDir(), // no PKGBUILD sentinel here
		ProjectDir:  t.TempDir(), // nor here
		LocalPkg:    lp,
	}
	_, err := RenderLocalPkgImageInstall(s, true, t.TempDir(), "charly-arch")
	if err == nil {
		t.Fatalf("dev-local-pkg with no source dir must HARD ERROR (no silent fallback to the release); got nil")
	}
	if !strings.Contains(err.Error(), "dev-local-pkg") {
		t.Errorf("dev-mode error should name dev-local-pkg; got: %v", err)
	}
}
