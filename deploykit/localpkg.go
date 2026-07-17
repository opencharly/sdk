package deploykit

// localpkg.go — build a bundled package SOURCE dir on the host and install the
// resulting package FILE onto a deploy target, fully driven by the package
// format's `local_pkg:` config (the embedded build vocabulary (charly/charly.yml)
// `distro.<name>.format.<fmt>.local_pkg`). Relocated from charly core (W3): every
// function here operates ONLY on CandyModel/ResolvedBox-adjacent SDK types
// (LocalPkgInstallStep/BuilderStep/EmitOpts/DeployExecutor) plus host-fs/exec —
// no *Config, no live *Candy graph, no provider registry. The ONE genuine core
// dependency (resolving/ensuring a builder IMAGE is present — which may recurse
// into `charly box build` — a loader-coupled operation) is INJECTED as two
// closures (resolveImage/ensureImage) rather than threaded via *Config, so this
// file never imports charly core.
//
// This is the execution machinery behind LocalPkgInstallStep (the IR form of a
// candy's `localpkg:` field). NOTHING here hardcodes a package-format command:
// the source-dir sentinel, build command, install command, package-file glob,
// and probe command all come from the resolved *LocalPkgDef
// (LocalPkgInstallStep.LocalPkg / BuilderStep.LocalPkg), rendered through the
// EXISTING RenderTemplate engine (sdk/buildkit) — the same machinery the
// rest of the build pipeline uses. The install command is the format's
// AUTO-RESOLVING local-file install (pacman -U / dnf install / apt-get install),
// so the package's dependencies are satisfied from the target's repos and there
// is no dependency-closure to pre-build.
//
// Pieces, each a shared primitive (R3):
//
//   1. ResolveLocalPkgDir   — locate the package SOURCE directory from the
//      author's hint + the candy/project anchors (walk-up search), keyed on the
//      format's LocalPkgDef.SourceSentinel.
//   2. BuildLocalPkgOnHost  — render LocalPkgDef.BuildTemplate and run it on the
//      HOST, returning the produced package-file paths (globbed via PkgGlob).
//   3. TransferAndInstallPkgs — the SHARED transfer+install leg: PutFile each
//      package onto the target venue's filesystem (a local copy for the host
//      ShellExecutor, scp for the SSHExecutor) then render+run
//      LocalPkgDef.InstallTemplate via RunSystem. The SAME leg the aur-CANDY
//      deploy path uses (BuildDepPkgsOnHost → TransferAndInstallPkgs) — both
//      call this one helper.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
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

// localPkgBuildContext is the template context for LocalPkgDef.BuildTemplate.
type localPkgBuildContext struct {
	SrcDir  string // resolved package source directory (the PKGBUILD dir for pac)
	PkgDest string // per-build output dir the build writes package files into
}

// localPkgInstallContext is the template context for LocalPkgDef.InstallTemplate.
type localPkgInstallContext struct {
	StageDir string // on-target staging dir holding the transferred package files
	Glob     string // LocalPkgDef.PkgGlob (e.g. "*.pkg.tar.zst")
}

// ResolveLocalPkgDir locates the package SOURCE directory for a candy's
// `localpkg:` hint. Resolution order, returning the first directory that
// actually contains a `PKGBUILD` file:
//
//  1. absolute ref → used verbatim.
//  2. <candyDir>/<ref>     — the source bundled alongside the candy.
//  3. <projectDir>/<ref>   — relative to the deploy project dir (os.Getwd).
//  4. walk UP from projectDir, trying <ancestor>/<ref> at each level — this is
//     the operator path: `charly -C box/cachyos deploy add cachyos-gpu` has a
//     project dir of box/cachyos while pkg/arch lives at the SUPERPROJECT
//     root (../../pkg/arch). The walk finds it without the candy needing to
//     know how deeply the consuming project is nested.
//
// Returns "" when no PKGBUILD is found anywhere — the caller treats that as a
// no-op (the candy's own curl/COPY task is the documented fallback).
//
// The SOURCE-dir marker is the format's `source_sentinel` (PKGBUILD for pac,
// *.spec for rpm, debian/control for deb), matched via filepath.Glob so a plain
// filename, a sub-path, or a glob all work — no hardcoded format literal here.
func ResolveLocalPkgDir(ref, candyDir, projectDir, sentinel string) string {
	if ref == "" {
		return ""
	}
	hasSentinel := func(dir string) bool {
		if dir == "" || sentinel == "" {
			return false
		}
		// filepath.Glob handles a plain filename (PKGBUILD), a sub-path
		// (debian/control), and a glob (*.spec) uniformly: a meta-free pattern
		// returns the single literal when it exists.
		matches, err := filepath.Glob(filepath.Join(dir, sentinel))
		return err == nil && len(matches) > 0
	}

	if filepath.IsAbs(ref) {
		if hasSentinel(ref) {
			return ref
		}
		return ""
	}
	// Candy-relative, then project-relative.
	for _, base := range []string{candyDir, projectDir} {
		if base == "" {
			continue
		}
		if cand := filepath.Join(base, ref); hasSentinel(cand) {
			return cand
		}
	}
	// Walk up from the project dir. filepath.Dir is idempotent at the root
	// ("/" → "/"), so cap the loop to terminate even on an unrooted relative
	// projectDir.
	dir := projectDir
	for i := 0; dir != "" && i < 64; i++ {
		if cand := filepath.Join(dir, ref); hasSentinel(cand) {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// BuildLocalPkgOnHost builds the package(s) defined by the source dir on the
// HOST by rendering LocalPkgDef.BuildTemplate and returns the produced
// package-file paths (globbed via LocalPkgDef.PkgGlob). The build output lands
// in a per-call temp dir (passed as {{.PkgDest}}) so the glob is deterministic
// and the source tree is never polluted.
//
// The build command (e.g. makepkg) comes ENTIRELY from config — this function
// renders LocalPkgDef.BuildTemplate via the existing RenderTemplate engine and
// runs it under `bash -c`, so there is no hardcoded makepkg/pacman literal here.
//
// The temp dir is registered for sweep but deliberately NOT defer-removed: the
// caller owns the package files until install completes.
func BuildLocalPkgOnHost(ctx context.Context, lp *LocalPkgDef, srcDir string, opts EmitOpts) ([]string, error) {
	if lp == nil {
		return nil, fmt.Errorf("BuildLocalPkgOnHost: nil LocalPkgDef")
	}
	// Serialize concurrent builds of the SAME source dir (flock, cross-process):
	// makepkg materializes shared src/ working copies inside pkg/<fmt>, so two
	// concurrent builds would interleave in one directory — defensive mutual
	// exclusion; one shared source dir is the design (single source of truth).
	releaseLock, err := kit.AcquireLocalPkgBuildLock(srcDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = releaseLock() }()
	pkgDest, err := os.MkdirTemp("", "charly-localpkg-")
	if err != nil {
		return nil, fmt.Errorf("localpkg build output tempdir: %w", err)
	}
	vmshared.RegisterTempCleanup(pkgDest)

	buildCmd, err := buildkit.RenderTemplate("localpkg-build", lp.BuildTemplate, localPkgBuildContext{
		SrcDir:  srcDir,
		PkgDest: pkgDest,
	})
	if err != nil {
		return nil, fmt.Errorf("rendering localpkg build template: %w", err)
	}
	buildCmd = strings.TrimSpace(buildCmd)
	if buildCmd == "" {
		return nil, fmt.Errorf("localpkg build template rendered empty (format config missing build_template?)")
	}

	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] localpkg build (PKGDEST=%s): %s\n", pkgDest, buildCmd)
		return nil, nil
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", buildCmd)
	cmd.Stdout = os.Stderr // surface build output (operator debugging) without polluting stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("localpkg build in %s: %w", srcDir, err)
	}

	matches, _ := filepath.Glob(filepath.Join(pkgDest, lp.PkgGlob))
	if len(matches) == 0 {
		return nil, fmt.Errorf("localpkg build in %s produced no %s in %s", srcDir, lp.PkgGlob, pkgDest)
	}
	return matches, nil
}

// BuildDepPkgsOnHost builds an arbitrary set of dependency packages into
// package files ON THE HOST (where podman is available) through the EXISTING
// builder named by LocalPkgDef.DepBuilder (the `aur` builder for pac) and
// returns the produced package paths. It is the BUILD half of the VM target's
// aur `execBuilder` path factored out (R3): execBuilder now calls this and then
// TransferAndInstallPkgs, and the localpkg step calls it to build the package's
// dependency closure. There is exactly ONE host-side dep-builder implementation
// across the candy-aur path and the localpkg-dep-closure path.
//
// It synthesizes a BuilderStep{Builder:lp.DepBuilder, …} carrying the package
// names in RawStageContext["packages"], renders the SAME renderBuilderScript the
// container/local/VM builder paths use, wraps it with the same root
// backstop-find + chown-to-0:0 (so the bind-mount surface is host-readable under
// rootless podman), runs it via BuilderRun(RunAsRoot:true), surfaces output to
// stderr, and globs the staging dir for LocalPkgDef.PkgGlob.
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
func BuildDepPkgsOnHost(_ context.Context, lp *LocalPkgDef, bDef *BuilderDef, builderImage string, packages []string, candyDir string, resolveImage func(string) (string, error), ensureImage func(context.Context, string) error, opts EmitOpts) ([]string, error) {
	if len(packages) == 0 {
		return nil, nil
	}
	if lp == nil {
		return nil, fmt.Errorf("BuildDepPkgsOnHost: nil LocalPkgDef")
	}
	if builderImage == "" {
		return nil, fmt.Errorf("BuildDepPkgsOnHost: no %s builder image for packages %v", lp.DepBuilder, packages)
	}
	if bDef == nil {
		return nil, fmt.Errorf("BuildDepPkgsOnHost: no %s builder definition for packages %v", lp.DepBuilder, packages)
	}

	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] build %d dependency package(s) %v via %s builder %s\n",
			len(packages), packages, lp.DepBuilder, builderImage)
		return nil, nil
	}

	// Synthetic BuilderStep — the SAME shape the compiler produces, so
	// renderBuilderScript renders the identical build flow for this builder from
	// its phase.install.host cell (config-driven).
	step := &BuilderStep{
		Builder:         lp.DepBuilder,
		BuilderImage:    builderImage,
		BuilderDef:      bDef,
		CandyDir:        candyDir,
		Phase:           spec.PhaseInstall,
		RawStageContext: map[string]any{"packages": packages},
	}

	// Host staging dir bind-mounted as /tmp/aur-pkgs — the builder writes the
	// package files here; we then glob them. RegisterTempCleanup sweeps it on
	// exit; no defer-remove (caller owns the files until install completes).
	hostStage, err := os.MkdirTemp("", "charly-pkgdep-")
	if err != nil {
		return nil, fmt.Errorf("dependency staging mkdir: %w", err)
	}
	vmshared.RegisterTempCleanup(hostStage)

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
		"  find / -name " + kit.ShellQuote(lp.PkgGlob) + " 2>/dev/null -exec cp {} /tmp/aur-pkgs/ \\;\n" +
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
		return nil, fmt.Errorf("%s builder: %w", lp.DepBuilder, err)
	}

	matches, _ := filepath.Glob(filepath.Join(hostStage, lp.PkgGlob))
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s builder produced no %s in %s for packages %v", lp.DepBuilder, lp.PkgGlob, hostStage, packages)
	}
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

	install, err := buildkit.RenderTemplate("localpkg-install", lp.InstallTemplate, localPkgInstallContext{
		StageDir: localPkgGuestStage,
		Glob:     lp.PkgGlob,
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
		localPkgGuestStage, lp.PkgGlob)
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
// the external vm deploy call for a LocalPkgInstallStep: resolve the package source dir,
// build it on the host, then transfer+install onto the target venue. `supported`
// gates whether the install leg runs (the venue's package manager must match the
// step's format); an unsupported target or a missing source dir is a clean no-op
// (the candy's own curl/COPY task covers it).
//
// venueName is used only for log lines (e.g. "host", "vm:cachyos-gpu").
func ExecLocalPkgInstall(ctx context.Context, exec DeployExecutor, s *LocalPkgInstallStep, supported bool, venueName string, opts EmitOpts) error {
	if s.LocalPkg == nil {
		fmt.Fprintf(os.Stderr, "%s skip: localpkg %s (candy=%s) — target distro declares no localpkg-capable package format; the candy's curl/COPY task installs it instead\n",
			venueName, s.PkgbuildRef, s.CandyName)
		return nil
	}
	if !supported {
		fmt.Fprintf(os.Stderr, "%s skip: localpkg %s (candy=%s) — target has no %s package manager; the candy's curl/COPY task installs it instead\n",
			venueName, s.PkgbuildRef, s.CandyName, s.Format)
		return nil
	}
	pkgDir := ResolveLocalPkgDir(s.PkgbuildRef, s.CandyDir, s.ProjectDir, s.LocalPkg.SourceSentinel)
	if pkgDir == "" {
		fmt.Fprintf(os.Stderr, "%s skip: localpkg %s (candy=%s) — no package source found from candy dir %q or project dir %q; the candy's curl/COPY task installs it instead\n",
			venueName, s.PkgbuildRef, s.CandyName, s.CandyDir, s.ProjectDir)
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s: building %s package (%s) from %s for candy %s\n",
		venueName, strings.TrimSuffix(filepath.Base(pkgDir), "/"), s.Format, pkgDir, s.CandyName)
	pkgFiles, err := BuildLocalPkgOnHost(ctx, s.LocalPkg, pkgDir, opts)
	if err != nil {
		return fmt.Errorf("localpkg %s (candy=%s): %w", s.PkgbuildRef, s.CandyName, err)
	}
	if opts.DryRun {
		return nil
	}

	// Transfer + install. The format's install command auto-resolves the
	// package's dependencies from the target's repos (pacman -U / dnf install /
	// apt-get install), so there is no dependency-closure to pre-build.
	return TransferAndInstallPkgs(ctx, exec, s.LocalPkg, pkgFiles, opts)
}

// RenderLocalPkgImageInstall emits the IMAGE-build install of a candy's
// `localpkg:` package. It is the ONE place the check-vs-production charly-binary
// distinction lives (R3 — shared by every build-mode image-install path so it
// can never drift):
//
//   - PRODUCTION boxes (devLocalPkg=false) DOWNLOAD the candy's PUBLISHED package
//     (LocalPkgDef.DownloadTemplate → releases/latest, ${ARCH} resolved by
//     BuildKit) and install it. A real box ships the latest RELEASED toolchain.
//
//   - DISPOSABLE EVAL BEDS (devLocalPkg=true) BUILD the candy's package from the
//     LOCAL in-development source (LocalPkgDef.BuildTemplate, via the SAME
//     BuildLocalPkgOnHost the deploy path uses — R3), stage it into the image
//     build context, and COPY+install it. A bed thus ALWAYS tests the
//     in-development charly, never a stale published release.
//
// Both modes install via the SAME dep-resolving InstallTemplate (pacman -U /
// dnf install / apt-get install), so the toolchain is OS-tracked either way.
// Returns "" (no directive) when the format declares no localpkg contract (the
// candy's own task: install is the fallback).
func RenderLocalPkgImageInstall(s *LocalPkgInstallStep, devLocalPkg bool, imageDir, boxName string) (string, error) {
	lp := s.LocalPkg
	if lp == nil {
		return "", nil
	}
	install, err := buildkit.RenderTemplate("localpkg-install", lp.InstallTemplate, localPkgInstallContext{
		StageDir: localPkgGuestStage,
		Glob:     lp.PkgGlob,
	})
	if err != nil {
		return "", fmt.Errorf("rendering localpkg install template: %w", err)
	}
	install = strings.TrimSpace(install)
	if install == "" {
		return "", fmt.Errorf("localpkg install template rendered empty (format config missing install_template?)")
	}

	if devLocalPkg {
		return renderLocalPkgImageDevInstall(s, install, imageDir, boxName)
	}

	// PRODUCTION: download the published release package. No download_template →
	// no directive (the candy's own task: install is the fallback).
	if strings.TrimSpace(lp.DownloadTemplate) == "" {
		return "", nil
	}
	// Download to a glob-matching filename (e.g. "*.rpm" → "pkg.rpm") so the
	// install template's {{.StageDir}}/{{.Glob}} matches the downloaded file.
	pkgFile := "pkg" + strings.TrimPrefix(lp.PkgGlob, "*")
	return fmt.Sprintf("RUN mkdir -p %[1]s && curl -fsSL \"%[2]s\" -o %[1]s/%[3]s && %[4]s && rm -rf %[1]s\n",
		localPkgGuestStage, lp.DownloadTemplate, pkgFile, install), nil
}

// renderLocalPkgImageDevInstall is the DISPOSABLE-EVAL-BED leg of
// RenderLocalPkgImageInstall: build the candy's localpkg package from LOCAL
// in-development source on the host (the SAME BuildLocalPkgOnHost the deploy path
// uses — R3), stage it into the per-image build context (the charly source itself
// is excluded from the context, so the built package FILE is what the COPY
// reaches), and emit a COPY + the same dep-resolving install the download path
// runs. A missing source dir is a HARD ERROR — an check bed that cannot build the
// in-development package must fail loudly, never silently fall back to a release.
func renderLocalPkgImageDevInstall(s *LocalPkgInstallStep, install, imageDir, boxName string) (string, error) {
	lp := s.LocalPkg
	srcDir := ResolveLocalPkgDir(s.PkgbuildRef, s.CandyDir, s.ProjectDir, lp.SourceSentinel)
	if srcDir == "" {
		return "", fmt.Errorf("dev-local-pkg: cannot locate the %s localpkg source (%q) for candy %q — a disposable check bed must build the in-development package from local source", s.Format, s.PkgbuildRef, s.CandyName)
	}
	pkgFiles, err := BuildLocalPkgOnHost(context.Background(), lp, srcDir, EmitOpts{})
	if err != nil {
		return "", fmt.Errorf("dev-local-pkg: building %s package for candy %q from %s: %w", s.Format, s.CandyName, srcDir, err)
	}
	if len(pkgFiles) == 0 {
		return "", fmt.Errorf("dev-local-pkg: build produced no %s package for candy %q (glob %q)", s.Format, s.CandyName, lp.PkgGlob)
	}
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
