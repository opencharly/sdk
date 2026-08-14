package deploykit

// localpkg.go — obtain a candy's package FILE (the published release, or the
// in-development build) and install it onto a deploy target, fully driven by the
// package format's `local_pkg:` config (the embedded build vocabulary
// (charly/charly.yml) `distro.<name>.format.<fmt>.local_pkg`). Relocated from
// charly core (W3): every function here operates ONLY on
// CandyModel/ResolvedBox-adjacent SDK types
// (LocalPkgInstallStep/BuilderStep/EmitOpts/DeployExecutor) plus host-fs/exec —
// no *Config, no live *Candy graph, no provider registry. The ONE genuine core
// dependency (resolving/ensuring a builder IMAGE is present — which may recurse
// into `charly box build` — a loader-coupled operation) is INJECTED as two
// closures (resolveImage/ensureImage) rather than threaded via *Config, so this
// file never imports charly core.
//
// This is the execution machinery behind LocalPkgInstallStep (the IR form of a
// candy's `localpkg:` field). The old SOURCE-BUILD machinery (ResolveLocalPkgDir,
// BuildLocalPkgOnHost, the source-staging copiers) is GONE — the nFPM cutover
// replaced it with two package-FILE legs, both producing the same transfer+install
// shape:
//
//   1. PRODUCTION (opts.DevLocalPkg=false) — downloadLocalPkg downloads the
//      PUBLISHED package from the format's download_template URL (the distro
//      repo's stable copy) on the HOST, then TransferAndInstallPkgs ships it to
//      the target venue. No package source is needed at deploy time.
//   2. DISPOSABLE EVAL BED (opts.DevLocalPkg=true) — generateLocalPkg invokes the
//      `charly generate-packages` plugin (nFPM-based, sdk/packagekit) from the
//      IN-DEVELOPMENT binary + plugins (opts.LocalPkgBuild), then
//      TransferAndInstallPkgs ships the built package to the venue. A bed thus
//      ALWAYS tests the in-development charly, never a stale published release.
//
// The install command is the format's AUTO-RESOLVING local-file install
// (pacman -U / dnf install / apt-get install), so the package's dependencies are
// satisfied from the target's repos and there is no dependency-closure to
// pre-build. The package-file glob the install template matches is DERIVED from
// the download_template URL (localPkgGlobFromDownload) — with PkgGlob gone from
// the schema, the URL's extension chain is the one stable signal shared by the
// download leg, the build leg, and the install leg.
//
// Pieces, each a shared primitive (R3):
//
//   1. localPkgGlobFromDownload — derive the package-file glob from the format's
//      download_template URL (e.g. "…/charly-${ARCH}.pkg.tar.zst" → "*.pkg.tar.zst").
//   2. downloadLocalPkg / generateLocalPkg — the two package-FILE producers
//      (published download vs in-development plugin build), both returning
//      package-file paths in a per-call temp dir.
//   3. TransferAndInstallPkgs — the SHARED transfer+install leg: PutFile each
//      package onto the target venue's filesystem (a local copy for the host
//      ShellExecutor, scp for the SSHExecutor) then render+run
//      LocalPkgDef.InstallTemplate via RunSystem. The SAME leg the aur-CANDY
//      deploy path uses (BuildDepPkgsOnHost → TransferAndInstallPkgs) — both
//      call this one helper.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/spec"
)

// hostBuilderContext is the template context for a builder's phase.install.host cell. The
// HOME/PIXI_CACHE_DIR/NPM_CONFIG_PREFIX/CARGO_HOME values are injected by BuilderRunOpts.Env
// (the cells read them as $HOME/$CARGO_HOME), so the only template-visible datum is the
// package list (consumed by the aur cell). Relocated from charly/deploy_host_helpers.go (W3) —
// pure, no *Config/registry dependency, needed by BuildDepPkgsOnHost below.
type hostBuilderContext struct {
	HostHome string
	Packages []string
}

// RenderBuilderScript turns a BuilderStep into the bash script that runs inside the builder
// container — the host-side (deploy) analog of the build-time multi-stage, fully config-driven:
// it renders the builder's phase.install.host cell via the SAME RenderTemplate engine
// (text/template). HOME/PIXI_CACHE_DIR/NPM_CONFIG_PREFIX/CARGO_HOME are injected by
// BuilderRunOpts.Env before the script starts. Shared by BuildDepPkgsOnHost (this file) and
// charly/builder_venue.go's runVenueHomeArtifactBuilder (still core — needs the injected
// image-resolve/ensure seam, a genuine loader dependency).
func RenderBuilderScript(s *BuilderStep, hostHome string) (string, error) {
	if s.BuilderDef == nil {
		return "", fmt.Errorf("builder %q: no builder definition (BuilderDef unset)", s.Builder)
	}
	tmpl := buildkit.BuilderPhaseTemplate(s.BuilderDef, spec.PhaseInstall, spec.VenueHostNative)
	if tmpl == "" {
		return "", fmt.Errorf("builder %q: no phase.install.host template in the embedded build vocabulary", s.Builder)
	}
	ctx := hostBuilderContext{
		HostHome: hostHome,
		Packages: ExtractStringSlice(s.RawStageContext, "packages"),
	}
	script, err := buildkit.RenderTemplate(s.Builder+"-host", tmpl, ctx)
	if err != nil {
		return "", fmt.Errorf("rendering %s host builder template: %w", s.Builder, err)
	}
	return script, nil
}

// localPkgGuestStage is the staging dir on the deploy target where the built
// packages land before the format's install command runs. Shared by the
// builder and localpkg paths so both clean up the same well-known location
// idempotently. (A staging PATH, not a package-format string — venue-agnostic.)
const localPkgGuestStage = "/tmp/charly-pkgs"

// localPkgInstallContext is the template context for LocalPkgDef.InstallTemplate.
type localPkgInstallContext struct {
	StageDir string // on-target staging dir holding the transferred package files
	Glob     string // package-file glob derived from the download_template (e.g. "*.pkg.tar.zst")
}

// localPkgGlobFromDownload derives the package-file glob from the format's
// download_template URL. The download URL names the published package file
// (e.g. "…/charly-${ARCH}.pkg.tar.zst"); the install template globs the staged
// dir for the same file shape ("*.pkg.tar.zst"). With PkgGlob gone from the
// schema (the generate-packages plugin builds packages now), the URL's extension
// chain is the one stable signal shared by the download leg, the build leg, and
// the install leg. Returns "" when the URL has no recognizable extension.
func localPkgGlobFromDownload(downloadTemplate string) string {
	base := filepath.Base(downloadTemplate)
	// filepath.Base("") is "." — an empty/root URL has no file to glob.
	if base == "" || base == "." || base == "/" {
		return ""
	}
	// Strip the ${ARCH} placeholder (and any separator before it) — the published
	// file is arch-specific, the glob is arch-agnostic.
	if i := strings.Index(base, "${"); i >= 0 {
		base = base[i:]
	}
	if i := strings.Index(base, "."); i >= 0 {
		return "*" + base[i:]
	}
	return ""
}

// charlyFormatToNFPM maps a charly package-format name (the embedded build
// vocabulary's format keys: pac/deb/rpm/apk) to the nFPM format name the
// generate-packages plugin's --formats flag takes (archlinux/deb/rpm/apk/ipk/msix).
// The dev-local-pkg build invokes the plugin, so the step's Format must be
// translated. Returns "" for a format the plugin cannot build (the caller errors
// loudly — never a silent skip).
func charlyFormatToNFPM(format string) string {
	switch format {
	case "pac":
		return "archlinux"
	case "deb", "rpm", "apk":
		return format
	default:
		return ""
	}
}

// generateLocalPkg builds the IN-DEVELOPMENT package for a disposable check bed
// via the `charly generate-packages` plugin (nFPM-based, sdk/packagekit),
// invoked from the in-development binary + plugins. build (nil → discovered
// defaults: os.Executable(), runtime.GOARCH, `<binary> version`, the baked
// plugins dir, `<CandyDir>/charly.yml`). Returns the produced package-file paths
// in a per-call temp dir. Shared by the deploy-time dev leg (ExecLocalPkgInstall)
// and the image-build dev leg (renderLocalPkgImageDevInstall) — R3.
func generateLocalPkg(ctx context.Context, s *LocalPkgInstallStep, build *spec.LocalPkgBuildContext, dryRun bool) ([]string, error) {
	if build == nil {
		build = &spec.LocalPkgBuildContext{}
	}
	binary := build.Binary
	if binary == "" {
		binary, _ = os.Executable()
	}
	pluginsDir := build.PluginsDir
	if pluginsDir == "" {
		pluginsDir = bakedPluginDir
	}
	calVer := build.CalVer
	if calVer == "" {
		out, err := exec.CommandContext(ctx, binary, "version").Output()
		if err != nil {
			return nil, fmt.Errorf("dev-local-pkg: resolving in-development CalVer from %q version: %w", binary, err)
		}
		calVer = strings.TrimSpace(string(out))
	}
	arch := build.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}
	candyYAML := build.CandyYAML
	if candyYAML == "" {
		candyYAML = filepath.Join(s.CandyDir, "charly.yml")
	}
	nfpmFormat := charlyFormatToNFPM(s.Format)
	if nfpmFormat == "" {
		return nil, fmt.Errorf("dev-local-pkg: no nFPM format for charly format %q (candy=%s)", s.Format, s.CandyName)
	}
	outDir, release, err := proc.MkdirTempHeld("", "charly-localpkg-")
	if err != nil {
		return nil, fmt.Errorf("dev-local-pkg: build output tempdir: %w", err)
	}
	defer release()
	args := []string{
		"generate-packages",
		"--candy", candyYAML,
		"--binary", binary,
		"--plugins", pluginsDir,
		"--version", calVer,
		"--arch", arch,
		"--formats", nfpmFormat,
		"--out", outDir,
	}
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] %s %s\n", binary, strings.Join(args, " "))
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = os.Stderr // surface plugin output (operator debugging) without polluting stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("dev-local-pkg: %s generate-packages: %w", binary, err)
	}
	glob := localPkgGlobFromDownload(s.LocalPkg.DownloadTemplate)
	if glob == "" {
		return nil, fmt.Errorf("dev-local-pkg: cannot derive package glob from download_template %q (candy=%s)", s.LocalPkg.DownloadTemplate, s.CandyName)
	}
	matches, _ := filepath.Glob(filepath.Join(outDir, glob))
	if len(matches) == 0 {
		return nil, fmt.Errorf("dev-local-pkg: generate-packages produced no %s in %s (candy=%s)", glob, outDir, s.CandyName)
	}
	return matches, nil
}

// downloadLocalPkg downloads the PUBLISHED package for a LocalPkgInstallStep from
// the format's download_template URL (the distro repo's stable "latest" copy) on
// the HOST — where curl lives — into a per-call temp dir, resolving the ${ARCH}
// placeholder to runtime.GOARCH (the production path builds for the host's own
// arch). Returns the downloaded package-file path. The transfer+install leg
// (TransferAndInstallPkgs) then ships it to the target venue — the same
// obtain-on-host → transfer → install shape the old source-build path used, with
// the download replacing the build.
func downloadLocalPkg(ctx context.Context, s *LocalPkgInstallStep, opts EmitOpts) ([]string, error) {
	url := strings.ReplaceAll(s.LocalPkg.DownloadTemplate, "${ARCH}", runtime.GOARCH)
	glob := localPkgGlobFromDownload(s.LocalPkg.DownloadTemplate)
	if glob == "" {
		return nil, fmt.Errorf("cannot derive package glob from download_template %q (candy=%s)", s.LocalPkg.DownloadTemplate, s.CandyName)
	}
	// The download filename must match the derived glob so the install template's
	// {{.StageDir}}/{{.Glob}} matches the downloaded file (e.g. "*.pkg.tar.zst" →
	// "pkg.pkg.tar.zst").
	pkgFile := "pkg" + strings.TrimPrefix(glob, "*")
	dir, release, err := proc.MkdirTempHeld("", "charly-localpkg-")
	if err != nil {
		return nil, fmt.Errorf("download tempdir: %w", err)
	}
	defer release()
	dst := filepath.Join(dir, pkgFile)
	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] download %s → %s\n", url, dst)
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, "curl", "-fsSL", url, "-o", dst)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	return []string{dst}, nil
}

// CleanupBuiltPackageFiles releases the temporary package directory returned by
// downloadLocalPkg, generateLocalPkg, or BuildDepPkgsOnHost after its final
// consumer has copied, transferred, or installed the artifacts. It refuses every
// path outside the two MkdirTemp namespaces under the process temp root, so a
// caller mistake can never broaden cleanup into an arbitrary directory.
func CleanupBuiltPackageFiles(pkgFiles []string) error {
	if len(pkgFiles) == 0 {
		return nil
	}
	tempRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return fmt.Errorf("resolve package temp root: %w", err)
	}
	dirs := make(map[string]struct{})
	for _, pkgFile := range pkgFiles {
		dir, err := filepath.Abs(filepath.Dir(pkgFile))
		if err != nil {
			return fmt.Errorf("resolve package artifact directory for %q: %w", pkgFile, err)
		}
		base := filepath.Base(dir)
		if filepath.Dir(dir) != tempRoot || (!strings.HasPrefix(base, "charly-localpkg-") && !strings.HasPrefix(base, "charly-pkgdep-")) {
			return fmt.Errorf("refusing to clean package artifact outside Charly temp namespaces: %s", dir)
		}
		dirs[dir] = struct{}{}
	}
	var cleanupErr error
	for dir := range dirs {
		info, statErr := os.Lstat(dir)
		if errors.Is(statErr, os.ErrNotExist) {
			proc.UnregisterTempCleanup(dir)
			continue
		}
		if statErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("inspect package artifact directory %s: %w", dir, statErr))
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("refusing to clean non-directory package artifact path %s", dir))
			continue
		}
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove package artifact directory %s: %w", dir, removeErr))
			continue
		}
		proc.UnregisterTempCleanup(dir)
	}
	return cleanupErr
}

// BuildDepPkgsOnHost builds an arbitrary set of dependency packages into
// package files ON THE HOST (where podman is available) through the EXISTING
// builder named by builderName (the `aur` builder for pac) and returns the
// produced package paths. It is the BUILD half of the VM target's aur
// `execBuilder` path factored out (R3): execBuilder now calls this and then
// TransferAndInstallPkgs, and the localpkg step calls it to build the package's
// dependency closure. There is exactly ONE host-side dep-builder implementation
// across the candy-aur path and the localpkg-dep-closure path.
//
// It synthesizes a BuilderStep{Builder:builderName, …} carrying the package
// names in RawStageContext["packages"], renders the SAME renderBuilderScript the
// container/local/VM builder paths use, wraps it with the same root
// backstop-find + chown-to-0:0 (so the bind-mount surface is host-readable under
// rootless podman), runs it via BuilderRun(RunAsRoot:true), surfaces output to
// stderr, and globs the staging dir for the package glob derived from the
// format's download_template.
//
// Empty packages → (nil, nil): a no-op, never an error. On DryRun it logs the
// plan and returns nil (no artifacts).
//
// resolveImage/ensureImage are INJECTED: resolving a namespace-qualified /
// short builder ref to a concrete image, and auto-building it on demand via
// `charly box build`, needs the still-core loader (*Config + project dir) — a
// genuine, isolated host dependency this file cannot (and should not) absorb.
// The caller supplies the closures; a nil pair means "no resolve/ensure" (the
// caller already resolved builderImage, or accepts a bare literal).
//
// The staging tmpdir is registered for sweep but deliberately NOT defer-removed:
// the caller owns the returned package files until install completes.
func BuildDepPkgsOnHost(_ context.Context, lp *LocalPkgDef, builderName string, bDef *BuilderDef, builderImage string, packages []string, candyDir string, resolveImage func(string) (string, error), ensureImage func(context.Context, string) error, opts EmitOpts) ([]string, error) {
	if len(packages) == 0 {
		return nil, nil
	}
	if lp == nil {
		return nil, fmt.Errorf("BuildDepPkgsOnHost: nil LocalPkgDef")
	}
	if builderImage == "" {
		return nil, fmt.Errorf("BuildDepPkgsOnHost: no %s builder image for packages %v", builderName, packages)
	}
	if bDef == nil {
		return nil, fmt.Errorf("BuildDepPkgsOnHost: no %s builder definition for packages %v", builderName, packages)
	}
	glob := localPkgGlobFromDownload(lp.DownloadTemplate)
	if glob == "" {
		return nil, fmt.Errorf("BuildDepPkgsOnHost: cannot derive package glob from download_template %q for packages %v", lp.DownloadTemplate, packages)
	}

	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] build %d dependency package(s) %v via %s builder %s\n",
			len(packages), packages, builderName, builderImage)
		return nil, nil
	}

	// Synthetic BuilderStep — the SAME shape the compiler produces, so
	// renderBuilderScript renders the identical build flow for this builder from
	// its phase.install.host cell (config-driven).
	step := &BuilderStep{
		Builder:         builderName,
		BuilderImage:    builderImage,
		BuilderDef:      bDef,
		CandyDir:        candyDir,
		Phase:           spec.PhaseInstall,
		RawStageContext: map[string]any{"packages": packages},
	}

	// Host staging dir bind-mounted as /tmp/aur-pkgs — the builder writes the
	// package files here; we then glob them. RegisterTempCleanup sweeps it on
	// exit; no defer-remove (caller owns the files until install completes).
	// Held for the builder container's lifetime — same exposure as the localpkg stage above: the
	// writes land inside the bind-mount, never touching this root's own mtime.
	hostStage, releaseHostStage, err := proc.MkdirTempHeld("", "charly-pkgdep-")
	if err != nil {
		return nil, fmt.Errorf("dependency staging mkdir: %w", err)
	}
	defer releaseHostStage()
	keepArtifacts := false
	defer func() {
		if !keepArtifacts {
			_ = os.RemoveAll(hostStage)
			proc.UnregisterTempCleanup(hostStage)
		}
	}()

	hostHome, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("UserHomeDir: %w", err)
	}
	bindMounts, err := kit.UserScopeBindMounts(hostHome)
	if err != nil {
		return nil, err
	}
	bindMounts["/tmp/aur-pkgs"] = hostStage
	envVars := kit.UserScopeEnv(hostHome)

	// RenderBuilderScript runs AS ROOT inside the builder (RunAsRoot=true): for
	// aur it writes the NOPASSWD-wheel sudoers, adds `user` to wheel, then
	// `sudo -u user`s the build. Run it directly as root — do NOT pre-drop.
	innerScript, err := RenderBuilderScript(step, hostHome)
	if err != nil {
		return nil, err
	}
	wrappedScript := "set -e\n" +
		innerScript + "\n" +
		"# Backstop find: the builder installs the package and cleans up its\n" +
		"# build tree, so the inner script's find may run after the tree is\n" +
		"# already wiped. Broaden the search if /tmp/aur-pkgs is still empty.\n" +
		"if [ -z \"$(ls -A /tmp/aur-pkgs 2>/dev/null)\" ]; then\n" +
		"  find / -name " + spec.ShellQuote(glob) + " 2>/dev/null -exec cp {} /tmp/aur-pkgs/ \\;\n" +
		"fi\n" +
		"# Rootless-podman userns fix: files created by container user\n" +
		"# 1000 land in the host's subuid range and become unreadable to\n" +
		"# the operator. chown to 0:0 — root in container maps to the\n" +
		"# host user under rootless podman — so the bind-mount surface is\n" +
		"# host-readable for the subsequent transfer+install leg.\n" +
		"chown -R 0:0 /tmp/aur-pkgs/\n"

	out, err := kit.BuilderRun(opts.ContextOrDefault(), BuilderRunOpts{
		BuilderImage: builderImage,
		CandyDir:     step.CandyDir,
		ScriptBody:   wrappedScript,
		BindMounts:   bindMounts,
		Env:          envVars,
		HostHome:     hostHome,
		DryRun:       opts.DryRun,
		RunAsRoot:    true,
		// Injected image resolve/ensure seams (see doc comment above) — nil-safe:
		// a caller that already resolved builderImage to a concrete ref, or
		// accepts it bare, passes nil for either/both.
		ResolveImage: resolveImage,
		EnsureImage:  ensureImage,
	})
	// Always surface the builder's stdout/stderr — the operator needs to see
	// compile output to debug build failures, not just the bare exit status.
	if len(out) > 0 {
		_, _ = os.Stderr.Write(out)
	}
	if err != nil {
		return nil, fmt.Errorf("%s builder: %w", builderName, err)
	}

	matches, _ := filepath.Glob(filepath.Join(hostStage, glob))
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s builder produced no %s in %s for packages %v", builderName, glob, hostStage, packages)
	}
	keepArtifacts = true
	return matches, nil
}

// TransferAndInstallPkgs ships built package files onto a deploy target and
// installs them by rendering LocalPkgDef.InstallTemplate. It is venue-agnostic
// via the DeployExecutor: PutFile is a local filesystem copy for the host
// ShellExecutor and an scp for the SSHExecutor, and RunSystem is local sudo vs
// `ssh sudo`. One implementation serves BOTH the localpkg step (the local deploy target
// / the external vm deploy) AND the builder's install leg (BuilderStep.LocalPkg), so
// "ship packages to a venue and install them" has a single config-driven home
// (R3).
//
// The staging dir is cleared before transfer so a re-run replaces stale content
// idempotently; the format's install command (e.g. `pacman -U`) is expected to
// be the upgrade form, so re-installing the same or a newer build never errors.
func TransferAndInstallPkgs(ctx context.Context, exec DeployExecutor, lp *LocalPkgDef, pkgFiles []string, opts EmitOpts) error {
	if lp == nil {
		return fmt.Errorf("TransferAndInstallPkgs: nil LocalPkgDef")
	}
	if len(pkgFiles) == 0 {
		return fmt.Errorf("TransferAndInstallPkgs: no package files to install")
	}
	glob := localPkgGlobFromDownload(lp.DownloadTemplate)
	if glob == "" {
		return fmt.Errorf("TransferAndInstallPkgs: cannot derive package glob from download_template %q", lp.DownloadTemplate)
	}

	install, err := buildkit.RenderTemplate("localpkg-install", lp.InstallTemplate, localPkgInstallContext{
		StageDir: localPkgGuestStage,
		Glob:     glob,
	})
	if err != nil {
		return fmt.Errorf("rendering localpkg install template: %w", err)
	}
	install = strings.TrimSpace(install)
	if install == "" {
		return fmt.Errorf("localpkg install template rendered empty (format config missing install_template?)")
	}

	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] transfer %d package(s) to %s and install on %s: %s\n",
			len(pkgFiles), localPkgGuestStage, exec.Venue(), install)
		return nil
	}

	prep := fmt.Sprintf("set -e\nmkdir -p %[1]s\nrm -f %[1]s/%[2]s 2>/dev/null || true\n",
		localPkgGuestStage, glob)
	if err := exec.RunUser(ctx, prep, opts); err != nil {
		return fmt.Errorf("preparing package staging dir on %s: %w", exec.Venue(), err)
	}

	for _, f := range pkgFiles {
		dst := filepath.Join(localPkgGuestStage, filepath.Base(f))
		// ownerRoot=false: /tmp staging is user-writable; the install command
		// (RunSystem, sudo) reads it.
		if err := exec.PutFile(ctx, f, dst, 0o644, false, opts); err != nil {
			return fmt.Errorf("transferring package %s to %s: %w", filepath.Base(f), exec.Venue(), err)
		}
	}

	if err := exec.RunSystem(ctx, install, opts); err != nil {
		return fmt.Errorf("installing packages on %s: %w", exec.Venue(), err)
	}
	return nil
}

// VenueHasPkgManager probes the actual deploy venue for the package format's
// manager — the precondition for executing a LocalPkgInstallStep. The probe
// command comes from LocalPkgDef.Probe (e.g. `command -v pacman`), so this is
// config-driven, not a hardcoded pacman literal. Probing the VENUE (not the host
// running charly) is what makes the gate correct for a VM deploy: the guest may be a
// different distro than the operator host, and vice-versa. The executor is the
// venue (ShellExecutor → host, SSHExecutor → guest), so one probe through it is
// venue-accurate for both targets (R3). DryRun assumes true so the planner shows
// the build+install it WOULD do. A nil LocalPkgDef, empty probe, probe error, or
// non-matching venue returns false: charly never assumes a target can take a package.
func VenueHasPkgManager(ctx context.Context, exec DeployExecutor, lp *LocalPkgDef, opts EmitOpts) bool {
	if lp == nil || strings.TrimSpace(lp.Probe) == "" {
		return false
	}
	if opts.DryRun {
		return true
	}
	probe := fmt.Sprintf("%s >/dev/null 2>&1 && echo yes || echo no", lp.Probe)
	stdout, _, _, err := exec.RunCapture(ctx, probe)
	if err != nil {
		return false
	}
	return strings.TrimSpace(stdout) == "yes"
}

// ExecLocalPkgInstall is the shared body both the local deploy target and
// the external vm deploy call for a LocalPkgInstallStep: obtain the package FILE
// (published download for a production deploy, in-development plugin build for a
// disposable eval bed), then transfer+install onto the target venue. `supported`
// gates whether the install leg runs (the venue's package manager must match the
// step's format); an unsupported target or a format with no download_template is
// a clean no-op (the candy's own curl/COPY task covers it).
//
// venueName is used only for log lines (e.g. "host", "vm:cachyos-gpu").
func ExecLocalPkgInstall(ctx context.Context, exec DeployExecutor, s *LocalPkgInstallStep, supported bool, venueName string, opts EmitOpts) error {
	if s.LocalPkg == nil {
		fmt.Fprintf(os.Stderr, "%s skip: localpkg %s (candy=%s) — target distro declares no localpkg-capable package format; the candy's curl/COPY task installs it instead\n",
			venueName, s.PackageName, s.CandyName)
		return nil
	}
	if !supported {
		fmt.Fprintf(os.Stderr, "%s skip: localpkg %s (candy=%s) — target has no %s package manager; the candy's curl/COPY task installs it instead\n",
			venueName, s.PackageName, s.CandyName, s.Format)
		return nil
	}

	var pkgFiles []string
	var err error
	if opts.DevLocalPkg {
		// DISPOSABLE EVAL BED: build the IN-DEVELOPMENT package via the
		// generate-packages plugin from the in-development binary + plugins.
		fmt.Fprintf(os.Stderr, "%s: building in-development %s package (%s) for candy %s via generate-packages\n",
			venueName, s.PackageName, s.Format, s.CandyName)
		pkgFiles, err = generateLocalPkg(ctx, s, opts.LocalPkgBuild, opts.DryRun)
	} else {
		// PRODUCTION: download the PUBLISHED package from the distro repo.
		if strings.TrimSpace(s.LocalPkg.DownloadTemplate) == "" {
			fmt.Fprintf(os.Stderr, "%s skip: localpkg %s (candy=%s) — format %s declares no download_template; the candy's curl/COPY task installs it instead\n",
				venueName, s.PackageName, s.CandyName, s.Format)
			return nil
		}
		fmt.Fprintf(os.Stderr, "%s: downloading published %s package (%s) for candy %s\n",
			venueName, s.PackageName, s.Format, s.CandyName)
		pkgFiles, err = downloadLocalPkg(ctx, s, opts)
	}
	if err != nil {
		return fmt.Errorf("localpkg %s (candy=%s): %w", s.PackageName, s.CandyName, err)
	}
	if opts.DryRun {
		return nil
	}
	if len(pkgFiles) == 0 {
		return fmt.Errorf("localpkg %s (candy=%s): no package files produced", s.PackageName, s.CandyName)
	}

	// Transfer + install. The format's install command auto-resolves the
	// package's dependencies from the target's repos (pacman -U / dnf install /
	// apt-get install), so there is no dependency-closure to pre-build.
	installErr := TransferAndInstallPkgs(ctx, exec, s.LocalPkg, pkgFiles, opts)
	cleanupErr := CleanupBuiltPackageFiles(pkgFiles)
	return errors.Join(installErr, cleanupErr)
}

// RenderLocalPkgImageInstall emits the IMAGE-build install of a candy's
// `localpkg:` package. It is the ONE place the check-vs-production charly-binary
// distinction lives (R3 — shared by every build-mode image-install path so it
// can never drift):
//
//   - PRODUCTION boxes (devLocalPkg=false) DOWNLOAD the candy's PUBLISHED package
//     (LocalPkgDef.DownloadTemplate → the distro repo's stable copy, ${ARCH}
//     resolved by BuildKit) and install it. A real box ships the latest RELEASED
//     toolchain.
//
//   - DISPOSABLE EVAL BEDS (devLocalPkg=true) BUILD the candy's package from the
//     IN-DEVELOPMENT binary + plugins via the `charly generate-packages` plugin
//     (localPkgBuild; nil → discovered defaults), stage it into the image build
//     context, and COPY+install it. A bed thus ALWAYS tests the in-development
//     charly, never a stale published release.
//
// Both modes install via the SAME dep-resolving InstallTemplate (pacman -U /
// dnf install / apt-get install), so the toolchain is OS-tracked either way.
// Returns "" (no directive) when the format declares no localpkg contract (the
// candy's own task: install is the fallback).
func RenderLocalPkgImageInstall(s *LocalPkgInstallStep, devLocalPkg bool, localPkgBuild *spec.LocalPkgBuildContext, imageDir, boxName string) (string, error) {
	lp := s.LocalPkg
	if lp == nil {
		return "", nil
	}
	// No download_template → no package-file contract at all: the candy's own
	// task: install is the fallback (both modes).
	if strings.TrimSpace(lp.DownloadTemplate) == "" {
		return "", nil
	}
	glob := localPkgGlobFromDownload(lp.DownloadTemplate)
	if glob == "" {
		return "", fmt.Errorf("localpkg install: cannot derive package glob from download_template %q (candy=%s)", lp.DownloadTemplate, s.CandyName)
	}
	install, err := buildkit.RenderTemplate("localpkg-install", lp.InstallTemplate, localPkgInstallContext{
		StageDir: localPkgGuestStage,
		Glob:     glob,
	})
	if err != nil {
		return "", fmt.Errorf("rendering localpkg install template: %w", err)
	}
	install = strings.TrimSpace(install)
	if install == "" {
		return "", fmt.Errorf("localpkg install template rendered empty (format config missing install_template?)")
	}

	if devLocalPkg {
		return renderLocalPkgImageDevInstall(s, install, localPkgBuild, imageDir, boxName)
	}

	// PRODUCTION: download the published release package. Download to a
	// glob-matching filename (e.g. "*.rpm" → "pkg.rpm") so the install template's
	// {{.StageDir}}/{{.Glob}} matches the downloaded file.
	pkgFile := "pkg" + strings.TrimPrefix(glob, "*")
	return fmt.Sprintf("RUN mkdir -p %[1]s && curl -fsSL \"%[2]s\" -o %[1]s/%[3]s && %[4]s && rm -rf %[1]s\n",
		localPkgGuestStage, lp.DownloadTemplate, pkgFile, install), nil
}

// renderLocalPkgImageDevInstall is the DISPOSABLE-EVAL-BED leg of
// RenderLocalPkgImageInstall: build the candy's localpkg package from the
// IN-DEVELOPMENT binary + plugins via the `charly generate-packages` plugin (the
// SAME generateLocalPkg the deploy path uses — R3), stage it into the per-image
// build context (the charly source itself is excluded from the context, so the
// built package FILE is what the COPY reaches), and emit a COPY + the same
// dep-resolving install the download path runs. A failed plugin build is a HARD
// ERROR — an check bed that cannot build the in-development package must fail
// loudly, never silently fall back to a release.
func renderLocalPkgImageDevInstall(s *LocalPkgInstallStep, install string, localPkgBuild *spec.LocalPkgBuildContext, imageDir, boxName string) (directive string, returnErr error) {
	pkgFiles, err := generateLocalPkg(context.Background(), s, localPkgBuild, false)
	if err != nil {
		return "", fmt.Errorf("dev-local-pkg: building %s package for candy %q: %w", s.Format, s.CandyName, err)
	}
	if len(pkgFiles) == 0 {
		return "", fmt.Errorf("dev-local-pkg: build produced no %s package for candy %q", s.Format, s.CandyName)
	}
	defer func() {
		returnErr = errors.Join(returnErr, CleanupBuiltPackageFiles(pkgFiles))
	}()
	// Stage the built package file(s) into the per-image build context so the
	// Containerfile COPY can reach them. Build into a per-process temp dir and
	// ATOMICALLY install it as the stage dir. This is load-bearing: the install
	// step GLOBS the dir (`dnf install /tmp/charly-pkgs/*.rpm` /
	// `pacman -U .../*.pkg.tar.zst`), so a STALE package from a prior generate
	// (a different CalVer of the same package) must NOT linger or the glob
	// matches two versions ("conflicting requests" / "duplicate target"). The
	// atomic swap replaces the whole dir with ONLY the current package(s) and
	// keeps a concurrent build's COPY race-free (no destructive in-place clean).
	stageRel := filepath.Join("_localpkg", s.CandyName)
	stageDir := filepath.Join(imageDir, stageRel)
	if err := os.MkdirAll(filepath.Dir(stageDir), 0o755); err != nil {
		return "", fmt.Errorf("dev-local-pkg: staging parent %s: %w", filepath.Dir(stageDir), err)
	}
	tmpStage, err := os.MkdirTemp(filepath.Dir(stageDir), "."+s.CandyName+".tmp.*")
	if err != nil {
		return "", fmt.Errorf("dev-local-pkg: staging temp dir: %w", err)
	}
	for _, pf := range pkgFiles {
		data, err := os.ReadFile(pf)
		if err != nil {
			_ = os.RemoveAll(tmpStage)
			return "", fmt.Errorf("dev-local-pkg: reading built package %s: %w", pf, err)
		}
		if err := os.WriteFile(filepath.Join(tmpStage, filepath.Base(pf)), data, 0o644); err != nil {
			_ = os.RemoveAll(tmpStage)
			return "", fmt.Errorf("dev-local-pkg: staging package %s: %w", filepath.Base(pf), err)
		}
	}
	if err := kit.InstallDirAtomic(tmpStage, stageDir); err != nil {
		return "", fmt.Errorf("dev-local-pkg: installing stage dir %s: %w", stageDir, err)
	}
	// COPY the staged package(s) into the image stage dir, then install via the
	// SAME dep-resolving install template the download path uses. COPY of a
	// trailing-slash dir copies its CONTENTS into the (auto-created) dest.
	copySrc := ".build/" + boxName + "/" + filepath.ToSlash(stageRel) + "/"
	return fmt.Sprintf("COPY %[1]s %[2]s/\nRUN %[3]s && rm -rf %[2]s\n",
		copySrc, localPkgGuestStage, install), nil
}
