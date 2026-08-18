package deploykit

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/spec"
)

// Tests for the localpkg subsystem relocated from charly core (W3): the two
// package-FILE legs (downloadLocalPkg / generateLocalPkg), the shared
// transfer+install leg (TransferAndInstallPkgs), the venue gate
// (VenueHasPkgManager), the deploy-time executor (ExecLocalPkgInstall), the
// image-build render (RenderLocalPkgImageInstall), and the dep-builder
// (BuildDepPkgsOnHost). Every test here is PURE — no *Config, no live *Candy
// graph, no provider registry — matching the source file's own design
// (sdk/deploykit/localpkg.go). The old SOURCE-BUILD machinery tests
// (ResolveLocalPkgDir / BuildLocalPkgOnHost / stageLocalPkgSource) are GONE
// with the machinery they proved — the nFPM cutover replaced source builds
// with the two package-FILE legs below.

// testPacLocalPkgDef returns a LocalPkgDef mirroring charly.yml's `pac.local_pkg`
// block — the config that drives the localpkg mechanism. Tests use it so they
// exercise the SAME config-driven path the loader produces, without parsing YAML.
// PkgGlob/SourceSentinel/BuildTemplate/DepBuilder are GONE from the schema (the
// generate-packages plugin builds packages now); the package-file glob is DERIVED
// from the download_template URL (localPkgGlobFromDownload).
func testPacLocalPkgDef() *LocalPkgDef {
	return &LocalPkgDef{
		DownloadTemplate: "https://opencharly.github.io/charly-arch/${ARCH}/charly-${ARCH}.pkg.tar.zst",
		InstallTemplate:  "pacman -U --noconfirm {{.StageDir}}/{{.Glob}}",
		Probe:            "command -v pacman",
	}
}

// writeFakeCurl writes a fake `curl` executable into a temp dir and prepends the
// dir to PATH. The fake writes the URL into the `-o` destination (so tests can
// assert the resolved URL), fails when the URL contains "fail", and errors on a
// missing -o/URL. downloadLocalPkg's exec.CommandContext("curl", …) resolves to
// it via PATH.
func writeFakeCurl(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
# fake curl for tests: writes the URL into the -o destination.
# downloadLocalPkg invokes: curl -fsSL <url> -o <dst>.
dst=""
url=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then dst="$arg"; fi
  if [ "$prev" = "-fsSL" ]; then url="$arg"; fi
  prev="$arg"
done
if [ -z "$dst" ] || [ -z "$url" ]; then
  echo "fake curl: missing -o or URL" >&2
  exit 2
fi
case "$url" in
  *fail*) echo "fake curl: download failed" >&2; exit 1 ;;
esac
printf 'package-content-from-%s\n' "$url" > "$dst"
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// writeFakeCharly writes a fake `charly` binary into a temp dir and returns its
// path. `version` prints a CalVer; `generate-packages` writes a package file
// matching the derived glob into --out, unless --arch is "none" (writes
// nothing) or "fail" (exits 1). Tests pass the path as LocalPkgBuildContext.Binary.
func writeFakeCharly(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
# fake charly binary for tests.
if [ "$1" = "version" ]; then
  echo "2026.225.1200"
  exit 0
fi
if [ "$1" = "generate-packages" ]; then
  out=""
  arch=""
  prev=""
  for arg in "$@"; do
    if [ "$prev" = "--out" ]; then out="$arg"; fi
    if [ "$prev" = "--arch" ]; then arch="$arg"; fi
    prev="$arg"
  done
  if [ -z "$out" ]; then
    echo "fake charly: generate-packages missing --out" >&2
    exit 2
  fi
  case "$arch" in
    none) ;; # write nothing — the caller must error on an empty glob match
    fail) echo "fake charly: generate-packages failed" >&2; exit 1 ;;
    *) printf 'package-content\n' > "$out/charly-2026.225.1200-1-x86_64.pkg.tar.zst" ;;
  esac
  exit 0
fi
echo "fake charly: unknown command: $*" >&2
exit 1
`
	path := filepath.Join(dir, "charly")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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

// TestLocalPkgGlobFromDownload proves the package-file glob is DERIVED from the
// download_template URL's extension chain (the one stable signal shared by the
// download leg, the build leg, and the install leg now that PkgGlob is gone).
func TestLocalPkgGlobFromDownload(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want string
	}{
		{"https://opencharly.github.io/charly-arch/${ARCH}/charly-${ARCH}.pkg.tar.zst", "*.pkg.tar.zst"},
		{"https://opencharly.github.io/charly-fedora/${ARCH}/charly-${ARCH}.rpm", "*.rpm"},
		{"https://opencharly.github.io/charly-debian/pool/main/c/charly/charly-${ARCH}.deb", "*.deb"},
		{"https://opencharly.github.io/charly-alpine/${ARCH}/charly-${ARCH}.apk", "*.apk"},
		{"https://opencharly.github.io/charly-openwrt/${ARCH}/charly-${ARCH}.ipk", "*.ipk"},
		// No ${ARCH} placeholder but a recognizable extension.
		{"https://example.com/charly.pkg.tar.zst", "*.pkg.tar.zst"},
		// No extension → no glob (the caller errors loudly, never a silent skip).
		{"https://example.com/charly", ""},
		{"", ""},
	} {
		if got := localPkgGlobFromDownload(tc.url); got != tc.want {
			t.Errorf("localPkgGlobFromDownload(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

// TestCharlyFormatToNFPM proves the charly format-key → nFPM format-name mapping
// the dev-local-pkg build uses to invoke the generate-packages plugin. A format
// the plugin cannot build maps to "" (the caller errors loudly).
func TestCharlyFormatToNFPM(t *testing.T) {
	for _, tc := range []struct {
		format string
		want   string
	}{
		{"pac", "archlinux"},
		{"deb", "deb"},
		{"rpm", "rpm"},
		{"apk", "apk"},
		{"ipk", ""}, // charly has no ipk format key in the build vocabulary
		{"", ""},
	} {
		if got := charlyFormatToNFPM(tc.format); got != tc.want {
			t.Errorf("charlyFormatToNFPM(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}

// TestDownloadLocalPkg proves the PRODUCTION leg downloads the published package
// from the format's download_template URL on the HOST (via curl), resolves the
// ${ARCH} placeholder to runtime.GOARCH, and names the file so it matches the
// derived glob (the install template's {{.StageDir}}/{{.Glob}} must match it).
func TestDownloadLocalPkg(t *testing.T) {
	writeFakeCurl(t)
	lp := testPacLocalPkgDef()
	s := &LocalPkgInstallStep{CandyName: "charly", Format: "pac", LocalPkg: lp}

	files, err := downloadLocalPkg(context.Background(), s, EmitOpts{})
	if err != nil {
		t.Fatalf("downloadLocalPkg: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("downloaded files = %v, want one", files)
	}
	// The filename must match the derived glob ("*.pkg.tar.zst" → "pkg.pkg.tar.zst").
	if base := filepath.Base(files[0]); base != "pkg.pkg.tar.zst" {
		t.Errorf("downloaded filename = %q, want pkg.pkg.tar.zst (glob-matching)", base)
	}
	// The URL was ${ARCH}-resolved to the host's own arch.
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	wantURL := strings.ReplaceAll(lp.DownloadTemplate, "${ARCH}", runtime.GOARCH)
	if !strings.Contains(string(data), wantURL) {
		t.Errorf("downloaded content = %q, want it to echo the resolved URL %q", data, wantURL)
	}
	// The temp dir is a Charly namespace (CleanupBuiltPackageFiles can release it).
	if !strings.HasPrefix(filepath.Base(filepath.Dir(files[0])), "charly-localpkg-") {
		t.Errorf("download dir = %q, want a charly-localpkg- namespace", filepath.Dir(files[0]))
	}
	if err := CleanupBuiltPackageFiles(files); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

// TestDownloadLocalPkg_DryRunAndFailure proves DryRun never shells out (no curl
// invocation, no artifacts) and a failing download is a loud error, never a
// silent skip.
func TestDownloadLocalPkg_DryRunAndFailure(t *testing.T) {
	writeFakeCurl(t)
	lp := testPacLocalPkgDef()
	s := &LocalPkgInstallStep{CandyName: "charly", Format: "pac", LocalPkg: lp}

	if files, err := downloadLocalPkg(context.Background(), s, EmitOpts{DryRun: true}); err != nil || files != nil {
		t.Errorf("dry-run = (%v, %v), want (nil, nil)", files, err)
	}

	failing := &LocalPkgInstallStep{CandyName: "charly", Format: "pac", LocalPkg: &LocalPkgDef{
		DownloadTemplate: "https://example.com/charly-${ARCH}.pkg.tar.zst/fail",
		InstallTemplate:  lp.InstallTemplate,
		Probe:            lp.Probe,
	}}
	if _, err := downloadLocalPkg(context.Background(), failing, EmitOpts{}); err == nil {
		t.Error("failing download returned nil error")
	}
}

// TestGenerateLocalPkg proves the DISPOSABLE-EVAL-BED leg invokes the
// generate-packages plugin from the in-development binary + plugins with the
// full flag surface, and globs the output dir for the derived package glob.
func TestGenerateLocalPkg(t *testing.T) {
	fakeCharly := writeFakeCharly(t)
	lp := testPacLocalPkgDef()
	s := &LocalPkgInstallStep{CandyName: "charly", CandyDir: t.TempDir(), Format: "pac", LocalPkg: lp}
	build := &spec.LocalPkgBuildContext{Binary: fakeCharly}

	files, err := generateLocalPkg(context.Background(), s, build, false)
	if err != nil {
		t.Fatalf("generateLocalPkg: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("built files = %v, want one", files)
	}
	if base := filepath.Base(files[0]); base != "charly-2026.225.1200-1-x86_64.pkg.tar.zst" {
		t.Errorf("built file = %q, want the fake binary's package", base)
	}
	if err := CleanupBuiltPackageFiles(files); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

// TestGenerateLocalPkg_DefaultsDiscovery proves the nil/partial build context
// discovers defaults: CalVer from `<binary> version`, PluginsDir from the baked
// plugins dir, CandyYAML from `<CandyDir>/charly.yml`, Arch from runtime.GOARCH.
func TestGenerateLocalPkg_DefaultsDiscovery(t *testing.T) {
	fakeCharly := writeFakeCharly(t)
	lp := testPacLocalPkgDef()
	s := &LocalPkgInstallStep{CandyName: "charly", CandyDir: t.TempDir(), Format: "pac", LocalPkg: lp}
	// Binary set, everything else empty → CalVer/PluginsDir/CandyYAML/Arch discovered.
	build := &spec.LocalPkgBuildContext{Binary: fakeCharly}

	files, err := generateLocalPkg(context.Background(), s, build, false)
	if err != nil {
		t.Fatalf("generateLocalPkg with defaults discovery: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("built files = %v, want one", files)
	}
	if err := CleanupBuiltPackageFiles(files); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

// TestGenerateLocalPkg_Errors proves the loud-failure contract: a format the
// plugin cannot build, a plugin that produces no matching files, and a plugin
// that fails all error — never a silent skip.
func TestGenerateLocalPkg_Errors(t *testing.T) {
	fakeCharly := writeFakeCharly(t)
	lp := testPacLocalPkgDef()

	// No nFPM format for the charly format key.
	noFormat := &LocalPkgInstallStep{CandyName: "charly", CandyDir: t.TempDir(), Format: "ipk", LocalPkg: lp}
	if _, err := generateLocalPkg(context.Background(), noFormat, &spec.LocalPkgBuildContext{Binary: fakeCharly}, false); err == nil {
		t.Error("format with no nFPM mapping should error")
	}

	// Plugin runs but produces no matching files (--arch none).
	noFiles := &LocalPkgInstallStep{CandyName: "charly", CandyDir: t.TempDir(), Format: "pac", LocalPkg: lp}
	if _, err := generateLocalPkg(context.Background(), noFiles, &spec.LocalPkgBuildContext{Binary: fakeCharly, Arch: "none"}, false); err == nil {
		t.Error("plugin producing no matching files should error")
	}

	// Plugin exits non-zero (--arch fail).
	failing := &LocalPkgInstallStep{CandyName: "charly", CandyDir: t.TempDir(), Format: "pac", LocalPkg: lp}
	if _, err := generateLocalPkg(context.Background(), failing, &spec.LocalPkgBuildContext{Binary: fakeCharly, Arch: "fail"}, false); err == nil {
		t.Error("failing plugin should error")
	}

	// DryRun: logs the plan, never shells out. (Format validation still runs
	// before the dry-run check — a dry-run must surface an unmappable format.)
	if files, err := generateLocalPkg(context.Background(), noFiles, &spec.LocalPkgBuildContext{Binary: fakeCharly}, true); err != nil || files != nil {
		t.Errorf("dry-run = (%v, %v), want (nil, nil)", files, err)
	}
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
// clean no-op: no download, no build, no transfer, no install — the candy's
// curl/COPY task installs it instead.
func TestExecLocalPkgInstall_SkipsUnsupported(t *testing.T) {
	exec := &localPkgRecExec{}
	s := &LocalPkgInstallStep{PackageName: "charly", CandyName: "charly", Format: "pac", LocalPkg: testPacLocalPkgDef()}
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
	s := &LocalPkgInstallStep{PackageName: "charly", CandyName: "charly"} // LocalPkg nil
	if err := ExecLocalPkgInstall(context.Background(), exec, s, true, "host", EmitOpts{}); err != nil {
		t.Fatalf("nil LocalPkg should be a clean no-op, got %v", err)
	}
	if len(exec.systemScripts) != 0 || len(exec.putDests) != 0 {
		t.Errorf("nil LocalPkg must not install anything: systemScripts=%v putDests=%v", exec.systemScripts, exec.putDests)
	}
}

// TestExecLocalPkgInstall_SkipsEmptyDownloadTemplate proves a PRODUCTION deploy
// whose format declares no download_template is a clean no-op (the candy's own
// curl/COPY task covers it) — the download leg has no URL to fetch.
func TestExecLocalPkgInstall_SkipsEmptyDownloadTemplate(t *testing.T) {
	exec := &localPkgRecExec{}
	lp := &LocalPkgDef{InstallTemplate: "pacman -U --noconfirm {{.StageDir}}/{{.Glob}}", Probe: "command -v pacman"} // no DownloadTemplate
	s := &LocalPkgInstallStep{PackageName: "charly", CandyName: "charly", Format: "pac", LocalPkg: lp}
	if err := ExecLocalPkgInstall(context.Background(), exec, s, true /* supported */, "host", EmitOpts{}); err != nil {
		t.Fatalf("empty download_template should be a clean no-op, got %v", err)
	}
	if len(exec.systemScripts) != 0 || len(exec.putDests) != 0 {
		t.Errorf("empty download_template must not install anything: systemScripts=%v putDests=%v", exec.systemScripts, exec.putDests)
	}
}

// TestExecLocalPkgInstall_ProductionDownloadsAndInstalls proves the PRODUCTION
// leg downloads the published package (fake curl) and ships it through the
// shared transfer+install leg (PutFile + rendered install command).
func TestExecLocalPkgInstall_ProductionDownloadsAndInstalls(t *testing.T) {
	writeFakeCurl(t)
	exec := &localPkgRecExec{}
	s := &LocalPkgInstallStep{PackageName: "charly", CandyName: "charly", Format: "pac", LocalPkg: testPacLocalPkgDef()}
	if err := ExecLocalPkgInstall(context.Background(), exec, s, true /* supported */, "host", EmitOpts{}); err != nil {
		t.Fatalf("production localpkg install: %v", err)
	}
	if len(exec.putDests) != 1 || !strings.HasPrefix(exec.putDests[0], localPkgGuestStage) {
		t.Errorf("package not staged under %s: %v", localPkgGuestStage, exec.putDests)
	}
	// The install command is rendered from the config template against the
	// DERIVED glob, not hardcoded.
	wantCmd := "pacman -U --noconfirm " + localPkgGuestStage + "/*.pkg.tar.zst"
	if len(exec.systemScripts) != 1 || strings.TrimSpace(exec.systemScripts[0]) != wantCmd {
		t.Errorf("install command = %v, want rendered %q", exec.systemScripts, wantCmd)
	}
}

// TestExecLocalPkgInstall_DevBuildsAndInstalls proves the DISPOSABLE-EVAL-BED
// leg builds the IN-DEVELOPMENT package via the generate-packages plugin (fake
// binary) and ships it through the same transfer+install leg.
func TestExecLocalPkgInstall_DevBuildsAndInstalls(t *testing.T) {
	fakeCharly := writeFakeCharly(t)
	exec := &localPkgRecExec{}
	s := &LocalPkgInstallStep{PackageName: "charly", CandyName: "charly", CandyDir: t.TempDir(), Format: "pac", LocalPkg: testPacLocalPkgDef()}
	opts := EmitOpts{DevLocalPkg: true, LocalPkgBuild: &spec.LocalPkgBuildContext{Binary: fakeCharly}}
	if err := ExecLocalPkgInstall(context.Background(), exec, s, true /* supported */, "vm:check-fedora-vm", opts); err != nil {
		t.Fatalf("dev-local-pkg install: %v", err)
	}
	if len(exec.putDests) != 1 || !strings.HasPrefix(exec.putDests[0], localPkgGuestStage) {
		t.Errorf("package not staged under %s: %v", localPkgGuestStage, exec.putDests)
	}
	wantCmd := "pacman -U --noconfirm " + localPkgGuestStage + "/*.pkg.tar.zst"
	if len(exec.systemScripts) != 1 || strings.TrimSpace(exec.systemScripts[0]) != wantCmd {
		t.Errorf("install command = %v, want rendered %q", exec.systemScripts, wantCmd)
	}
}

// TestExecLocalPkgInstall_DevFailureIsFatal proves a bed whose in-development
// package build fails is a HARD error — a bed must never silently skip the
// localpkg install and claim success (the regression the DevLocalPkg flag
// exists to close).
func TestExecLocalPkgInstall_DevFailureIsFatal(t *testing.T) {
	fakeCharly := writeFakeCharly(t)
	exec := &localPkgRecExec{}
	s := &LocalPkgInstallStep{PackageName: "charly", CandyName: "charly", CandyDir: t.TempDir(), Format: "pac", LocalPkg: testPacLocalPkgDef()}
	opts := EmitOpts{DevLocalPkg: true, LocalPkgBuild: &spec.LocalPkgBuildContext{Binary: fakeCharly, Arch: "fail"}}
	err := ExecLocalPkgInstall(context.Background(), exec, s, true /* supported */, "vm:check-fedora-vm", opts)
	if err == nil {
		t.Fatal("a bed whose in-development package build fails returned nil — the bed would install nothing and claim success")
	}
	for _, want := range []string{"dev-local-pkg", "charly"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q so the cause is readable at the point of failure", err, want)
		}
	}
	if len(exec.systemScripts) != 0 || len(exec.putDests) != 0 {
		t.Errorf("a failed bed localpkg install must not have installed anything: systemScripts=%v putDests=%v", exec.systemScripts, exec.putDests)
	}
}

// TestTransferAndInstallPkgs proves the shared transfer+install leg stages the
// dir, PutFiles each package, and renders the format's CONFIG-DRIVEN install
// command (LocalPkgDef.InstallTemplate) against the staging glob DERIVED from
// the download_template — venue-agnostic.
func TestTransferAndInstallPkgs(t *testing.T) {
	exec := &localPkgRecExec{}
	lp := testPacLocalPkgDef()
	pkgs := []string{"/tmp/build/charly-2026.225.1200-1-x86_64.pkg.tar.zst"}
	if err := TransferAndInstallPkgs(context.Background(), exec, lp, pkgs, EmitOpts{}); err != nil {
		t.Fatalf("TransferAndInstallPkgs: %v", err)
	}
	if len(exec.putDests) != 1 || !strings.HasPrefix(exec.putDests[0], localPkgGuestStage) {
		t.Errorf("package not staged under %s: %v", localPkgGuestStage, exec.putDests)
	}
	// The install command is rendered from the config template against the
	// DERIVED glob, not hardcoded.
	wantCmd := "pacman -U --noconfirm " + localPkgGuestStage + "/*.pkg.tar.zst"
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
	// No download_template → no derivable glob → error.
	noGlob := &LocalPkgDef{InstallTemplate: lp.InstallTemplate, Probe: lp.Probe}
	if err := TransferAndInstallPkgs(context.Background(), exec, noGlob, pkgs, EmitOpts{}); err == nil {
		t.Error("TransferAndInstallPkgs(no download_template) should error")
	}
}

// TestBuildDepPkgsOnHost_DryRunAndEmpty proves the dep-builder leg validates its
// inputs (empty packages is a no-op; nil/empty config errors rather than
// silently building) and honors DryRun (no shell-out). The glob is DERIVED from
// the format's download_template.
func TestBuildDepPkgsOnHost_DryRunAndEmpty(t *testing.T) {
	lp := testPacLocalPkgDef()
	bDef := &BuilderDef{}
	// Empty packages → (nil, nil): a no-op, never an error.
	if pkgs, err := BuildDepPkgsOnHost(context.Background(), lp, "aur", bDef, "arch-builder", nil, "/src", nil, nil, EmitOpts{}); err != nil || pkgs != nil {
		t.Errorf("empty packages = (%v, %v), want (nil, nil)", pkgs, err)
	}
	// DryRun: logs the plan, never shells out.
	if pkgs, err := BuildDepPkgsOnHost(context.Background(), lp, "aur", bDef, "arch-builder", []string{"qemu-full"}, "/src", nil, nil, EmitOpts{DryRun: true}); err != nil || pkgs != nil {
		t.Errorf("dry-run = (%v, %v), want (nil, nil)", pkgs, err)
	}
	// Nil LocalPkgDef → error.
	if _, err := BuildDepPkgsOnHost(context.Background(), nil, "aur", bDef, "arch-builder", []string{"qemu-full"}, "/src", nil, nil, EmitOpts{}); err == nil {
		t.Error("BuildDepPkgsOnHost(nil LocalPkgDef) should error")
	}
	// Empty builder image → error.
	if _, err := BuildDepPkgsOnHost(context.Background(), lp, "aur", bDef, "", []string{"qemu-full"}, "/src", nil, nil, EmitOpts{}); err == nil {
		t.Error("BuildDepPkgsOnHost(empty builder image) should error")
	}
	// Nil builder definition → error.
	if _, err := BuildDepPkgsOnHost(context.Background(), lp, "aur", nil, "arch-builder", []string{"qemu-full"}, "/src", nil, nil, EmitOpts{}); err == nil {
		t.Error("BuildDepPkgsOnHost(nil builder def) should error")
	}
	// No download_template → no derivable glob → error.
	noGlob := &LocalPkgDef{InstallTemplate: lp.InstallTemplate, Probe: lp.Probe}
	if _, err := BuildDepPkgsOnHost(context.Background(), noGlob, "aur", bDef, "arch-builder", []string{"qemu-full"}, "/src", nil, nil, EmitOpts{}); err == nil {
		t.Error("BuildDepPkgsOnHost(no download_template) should error")
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
	proc.RegisterTempCleanup(dir)
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

// RenderLocalPkgImageInstall: a PRODUCTION box build DOWNLOADS the candy's
// PUBLISHED release package (latest released toolchain) and installs it via the
// shared install template — never a COPY of a locally-built package.
func TestRenderLocalPkgImageInstall_ProductionDownloadsRelease(t *testing.T) {
	lp := testPacLocalPkgDef()
	s := &LocalPkgInstallStep{CandyName: "charly", Format: "pac", LocalPkg: lp}
	got, err := RenderLocalPkgImageInstall(s, false, nil, "", "")
	if err != nil {
		t.Fatalf("production render: %v", err)
	}
	if !strings.Contains(got, "curl -fsSL") || !strings.Contains(got, "charly-${ARCH}.pkg.tar.zst") {
		t.Errorf("production mode must DOWNLOAD the published release; got:\n%s", got)
	}
	if !strings.Contains(got, "pacman -U --noconfirm") {
		t.Errorf("production mode must install via the format install_template; got:\n%s", got)
	}
	if strings.Contains(got, "COPY ") {
		t.Errorf("production mode must NOT COPY a locally-built package; got:\n%s", got)
	}
}

// RenderLocalPkgImageInstall: a format with no download_template (or no
// LocalPkg at all) is a clean no-op in BOTH modes — the candy's own task:
// install is the fallback.
func TestRenderLocalPkgImageInstall_EmptyDownloadTemplateNoop(t *testing.T) {
	// Nil LocalPkg → "".
	if got, err := RenderLocalPkgImageInstall(&LocalPkgInstallStep{CandyName: "charly"}, false, nil, "", ""); err != nil || got != "" {
		t.Errorf("nil LocalPkg = (%q, %v), want (\"\", nil)", got, err)
	}
	// Empty download_template → "" in both modes.
	lp := &LocalPkgDef{InstallTemplate: "pacman -U --noconfirm {{.StageDir}}/{{.Glob}}", Probe: "command -v pacman"}
	s := &LocalPkgInstallStep{CandyName: "charly", Format: "pac", LocalPkg: lp}
	for _, dev := range []bool{false, true} {
		if got, err := RenderLocalPkgImageInstall(s, dev, nil, "", ""); err != nil || got != "" {
			t.Errorf("empty download_template (dev=%v) = (%q, %v), want (\"\", nil)", dev, got, err)
		}
	}
}

// RenderLocalPkgImageInstall: a DISPOSABLE check bed (devLocalPkg=true) builds
// the in-development package via the generate-packages plugin (fake binary),
// stages it into the image build context, and emits a COPY + the same
// dep-resolving install.
func TestRenderLocalPkgImageInstall_DevBuildsAndCopies(t *testing.T) {
	fakeCharly := writeFakeCharly(t)
	lp := testPacLocalPkgDef()
	s := &LocalPkgInstallStep{CandyName: "charly", CandyDir: t.TempDir(), Format: "pac", LocalPkg: lp}
	imageDir := t.TempDir()
	build := &spec.LocalPkgBuildContext{Binary: fakeCharly}

	got, err := RenderLocalPkgImageInstall(s, true, build, imageDir, "charly-arch")
	if err != nil {
		t.Fatalf("dev render: %v", err)
	}
	if !strings.Contains(got, "COPY ") || !strings.Contains(got, "pacman -U --noconfirm") {
		t.Errorf("dev mode must COPY the built package + install via the template; got:\n%s", got)
	}
	if strings.Contains(got, "curl -fsSL") {
		t.Errorf("dev mode must NOT download the published release; got:\n%s", got)
	}
	// The built package was staged into the image build context.
	staged := filepath.Join(imageDir, "_localpkg", "charly", "charly-2026.225.1200-1-x86_64.pkg.tar.zst")
	if _, err := os.Stat(staged); err != nil {
		t.Errorf("built package not staged at %s: %v", staged, err)
	}
}

// RenderLocalPkgImageInstall: a DISPOSABLE check bed whose in-development
// package build FAILS is a HARD ERROR — it must NEVER silently fall back to the
// published release (R4: no black-magic fallback that would let a bed test a
// stale binary).
func TestRenderLocalPkgImageInstall_DevFailureHardErrors(t *testing.T) {
	lp := testPacLocalPkgDef()
	s := &LocalPkgInstallStep{
		CandyName: "charly",
		Format:    "pac",
		CandyDir:  t.TempDir(),
		LocalPkg:  lp,
	}
	// A binary that cannot run (missing) with a pinned CalVer (so the failure is
	// the generate-packages invocation, not the version probe).
	build := &spec.LocalPkgBuildContext{Binary: "/no/such/binary", CalVer: "2026.225.1200"}
	_, err := RenderLocalPkgImageInstall(s, true, build, t.TempDir(), "charly-arch")
	if err == nil {
		t.Fatalf("dev-local-pkg with a failing build must HARD ERROR (no silent fallback to the release); got nil")
	}
	if !strings.Contains(err.Error(), "dev-local-pkg") {
		t.Errorf("dev-mode error should name dev-local-pkg; got: %v", err)
	}
}

// writeProjectFile creates dir/name (with parents) holding body — the scaffolding for the
// project trees the child-scope test needs.
func writeProjectFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFakeCharlyScoped wraps writeFakeCharly's package-building fake with the SCOPE GATE the
// real CLI applies before it ever reaches the packaging work, so a test can assert the child's
// invocation instead of only its output. The gate mirrors three real mechanisms:
//
//   - `generate-packages` is an out-of-process COMMAND plugin whose word enters the Kong grammar
//     only by pre-scanning the candies of the child's OWN project, so a project that declares no
//     such candy rejects the verb outright (`unexpected argument generate-packages`);
//   - CHARLY_PROJECT_DIR and CHARLY_PROJECT_REPO are mutually exclusive — a child handed both
//     exits before it parses anything;
//   - the child chdirs into its project, so a path argument still relative on arrival names a
//     different file there than the parent meant.
//
// It delegates to the plain fake for the actual build, so the two share one implementation.
func writeFakeCharlyScoped(t *testing.T) string {
	t.Helper()
	plain := writeFakeCharly(t)
	dir := t.TempDir()
	script := `#!/bin/sh
# fake charly with the CLI's project-scope gate in front of the plain fake.
if [ -n "$CHARLY_PROJECT_REPO" ] && [ -n "$CHARLY_PROJECT_DIR" ]; then
  echo "charly: --repo and --dir are mutually exclusive" >&2
  exit 2
fi
proj="${CHARLY_PROJECT_DIR:-$PWD}"
for arg in "$@"; do
  case "$arg" in
    --candy|--binary|--plugins|--out) expect_path=1; continue ;;
  esac
  if [ "$expect_path" = 1 ]; then
    expect_path=0
    case "$arg" in
      /*) ;;
      *) echo "charly: relative path argument $arg (cwd $PWD)" >&2; exit 2 ;;
    esac
  fi
done
if [ "$1" = "generate-packages" ] && [ ! -f "$proj/candy/generate-packages/charly.yml" ]; then
  echo "charly: error: unexpected argument generate-packages" >&2
  exit 1
fi
exec "` + plain + `" "$@"
`
	path := filepath.Join(dir, "charly")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestGenerateLocalPkg_ChildRunsInCandysProject is the regression guard for the packaging child's
// project scope. Each case reproduces a parent state that a real bed run leaves behind, and proves
// the child is nonetheless run under the project that OWNS the candy it is packaging, with the
// inherited scope stripped and every path argument absolute.
//
// Before the fix the child inherited CHARLY_PROJECT_DIR (and cwd) from the parent, so on every bed
// whose image composes candy/charly from a box submodule the word `generate-packages` was never in
// the child's grammar and --dev-local-pkg died at image-build time with
// `charly: error: unexpected argument generate-packages`.
func TestGenerateLocalPkg_ChildRunsInCandysProject(t *testing.T) {
	cases := []struct {
		name string
		// dirEnv/repoEnv are the parent's project-scope environment; cwdIsSubmodule says whether
		// the parent had already chdir'd into the box submodule (main() chdirs on -C/--dir).
		dirEnv, repoEnv string
		cwdIsSubmodule  bool
	}{
		// `charly -C box/cachyos check run <bed>` from the superproject root: main() sets the env
		// from the flag and chdirs. This is the reported failure — the child looked for its verb
		// in a submodule that vendors no candies.
		{name: "dir flag and chdir", dirEnv: "submodule", cwdIsSubmodule: true},
		// cwd genuinely elsewhere: the env names the submodule while the process sits somewhere
		// unrelated, so neither source can be trusted to name the candy's project.
		{name: "dir env with unrelated cwd", dirEnv: "submodule"},
		// `cd box/cachyos && charly check run <bed>`: no env at all, scope carried by cwd alone.
		{name: "cwd only", cwdIsSubmodule: true},
		// A --repo parent: both variables present in the child would abort it on
		// "--repo and --dir are mutually exclusive" the moment we set a dir of our own.
		{name: "repo env", repoEnv: "opencharly/charly", cwdIsSubmodule: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The project that owns the charly candy AND declares the generate-packages command
			// plugin.
			owner := t.TempDir()
			writeProjectFile(t, owner, spec.UnifiedFileName, "discover:\n    - path: candy\n")
			writeProjectFile(t, filepath.Join(owner, "candy", "generate-packages"), spec.UnifiedFileName, "generate-packages:\n")
			candyDir := filepath.Join(owner, "candy", "charly")
			writeProjectFile(t, candyDir, spec.UnifiedFileName, "charly:\n")

			// The box submodule the bed is actually being built from: a project of its own,
			// vendoring no candies — so it declares no command plugin and cannot serve the verb.
			submodule := t.TempDir()
			writeProjectFile(t, submodule, spec.UnifiedFileName, "boxes:\n")

			cwd := t.TempDir() // "genuinely elsewhere" unless the case chdirs into the submodule
			if tc.cwdIsSubmodule {
				cwd = submodule
			}
			relPlugins := "plugins-rel"
			writeProjectFile(t, filepath.Join(cwd, relPlugins), "keep", "")

			// The parent's state at the moment it spawns the child.
			t.Chdir(cwd)
			t.Setenv(spec.ProjectDirEnv, "")
			t.Setenv(spec.ProjectRepoEnv, "")
			if tc.dirEnv != "" {
				t.Setenv(spec.ProjectDirEnv, submodule)
			}
			if tc.repoEnv != "" {
				t.Setenv(spec.ProjectRepoEnv, tc.repoEnv)
			}

			s := &LocalPkgInstallStep{CandyName: "charly", CandyDir: candyDir, Format: "pac", LocalPkg: testPacLocalPkgDef()}
			// PluginsDir relative to the parent's cwd — it must reach the child absolute, because
			// the child resolves its own project and chdirs away from here.
			build := &spec.LocalPkgBuildContext{Binary: writeFakeCharlyScoped(t), PluginsDir: relPlugins}

			files, err := generateLocalPkg(context.Background(), s, build, false)
			if err != nil {
				t.Fatalf("generateLocalPkg from a box-submodule parent scope: %v", err)
			}
			if len(files) != 1 {
				t.Fatalf("built files = %v, want one", files)
			}
			if err := CleanupBuiltPackageFiles(files); err != nil {
				t.Fatalf("cleanup: %v", err)
			}
		})
	}
}

// TestCandyProjectDir proves the scope resolution itself: the nearest ANCESTOR holding a
// charly.yml wins, the candy's own manifest never counts as its project, and a candy with no
// enclosing project resolves to "" (the child then runs with no project scope at all rather than
// inheriting the parent's).
func TestCandyProjectDir(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, spec.UnifiedFileName, "boxes:\n")
	candyDir := filepath.Join(root, "candy", "charly")
	writeProjectFile(t, candyDir, spec.UnifiedFileName, "charly:\n")
	if got := candyProjectDir(filepath.Join(candyDir, spec.UnifiedFileName)); got != root {
		t.Errorf("candyProjectDir = %q, want %q", got, root)
	}

	// A nested project (a worktree, or a submodule with its own charly.yml) wins over the outer one.
	inner := filepath.Join(root, "box", "cachyos")
	writeProjectFile(t, inner, spec.UnifiedFileName, "boxes:\n")
	innerCandy := filepath.Join(inner, "candy", "local-thing")
	writeProjectFile(t, innerCandy, spec.UnifiedFileName, "local-thing:\n")
	if got := candyProjectDir(filepath.Join(innerCandy, spec.UnifiedFileName)); got != inner {
		t.Errorf("candyProjectDir nested = %q, want %q", got, inner)
	}

	// No enclosing project.
	orphan := t.TempDir()
	writeProjectFile(t, orphan, spec.UnifiedFileName, "orphan:\n")
	if got := candyProjectDir(filepath.Join(orphan, spec.UnifiedFileName)); got != "" {
		t.Errorf("candyProjectDir orphan = %q, want \"\"", got)
	}
}
