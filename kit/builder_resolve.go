package kit

// builder_resolve.go — the SINGLE shared implementation of the four detection-builders'
// BUILD-TIME multi-stage render (pixi / npm / aur / cargo), R3. It is the build-time
// counterpart of builder.go's DEPLOY-time legs (BuilderCollectContext / BuilderReverse):
// where those carry the per-candy stage context + teardown ops out-of-process, THIS renders
// the multi-stage build itself out-of-process (via each builder plugin's OpResolve leg), so
// a builder's build-time multi-stage is resolved BY THE PLUGIN — no longer the in-core
// embedded builder: vocabulary (the former generate.go emitBuilderStages / emitBuilderArtifacts
// StageTemplate render).
//
// The four stage templates below are the VERBATIM former embedded builder: vocabulary
// stage_template / install_template strings (charly/charly.yml), relocated here as the ONE
// source both the OUT-OF-PROCESS box-build path (the plugin's OpResolve) and the IN-PROC
// pod-overlay build-emit (stepEmitBuilder) render. The ONLY change from the vocab text: the
// two cache-mount template FUNC calls ({{cacheMountsOwned …}} / {{cacheMountsAuto …}}) became
// pre-rendered input fields ({{.CacheMountsOwned}} / {{.CacheMountsAuto}}) — the HOST renders
// the cache-mount flag strings (it owns the cache_mount vocab + the RenderCacheMounts helper)
// and passes them in BuilderResolveInput, so kit needs no cache-mount render engine and the
// emitted bytes stay byte-identical to the former embedded-vocabulary render.
//
// Selection stays DETECTION host-side (candyNeedsBuilder against the retained detect_file /
// detect_config vocabulary); the host computes the full BuilderResolveInput (builder ref,
// stage name, target identity, filesystem-detected manifest/lockfile/build-script, aur
// packages/options, pre-rendered cache mounts) and Invokes the plugin's OpResolve; the plugin
// returns the rendered Stage + CopyArtifacts (+ CopyBinary / InlineFragment) via THIS function.

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/opencharly/spec/spec"
)

// pixiStageTemplate is the verbatim former builder.pixi.stage_template (cache-mount func →
// pre-rendered {{.CacheMountsOwned}}).
const pixiStageTemplate = "FROM {{.BuilderRef}} AS {{.StageName}}\nUSER {{.UID}}\nWORKDIR {{.Home}}\n{{- if .HasLockFile}}\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/pixi.lock pixi.lock\n{{- end}}\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/{{.Manifest}} {{.Manifest}}\n{{.ManylinuxFix}}\nENV PIXI_CACHE_DIR=/tmp/pixi-cache RATTLER_CACHE_DIR=/tmp/rattler-cache\n{{- if .HasBuildScript}}\n# build.sh is COPY'd (NOT bind-mounted) so its CONTENT is part of this\n# stage's BuildKit cache key — editing build.sh (e.g. the pixelflux\n# NVENC patch) MUST invalidate the compile. A\n# `--mount=type=bind,from=<stage>,source=/build.sh` delivers the file\n# but its content NEVER enters the RUN cache key (the key is parent-SHA\n# + COPY'd manifest + RUN-text), so a changed build.sh silently reused\n# a stale compiled artifact — the \"new code not picked up\" bug. COPY\n# keys it exactly like pixi.toml / pixi.lock above.\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/{{.BuildScript}} /tmp/{{.BuildScript}}\nRUN {{.CacheMountsOwned}}{{.InstallCmd}} && bash /tmp/{{.BuildScript}} && rm -f {{.Manifest}} pixi.lock\n{{- else}}\nRUN {{.CacheMountsOwned}}{{.InstallCmd}} && rm -f {{.Manifest}} pixi.lock\n{{- end}}\n"

// npmStageTemplate is the verbatim former builder.npm.stage_template.
const npmStageTemplate = "FROM {{.BuilderRef}} AS {{.StageName}}\nUSER {{.UID}}\nWORKDIR {{.Home}}\n# Override NPM_CONFIG_PREFIX from the builder image so npm writes to\n# the TARGET image's HOME (not the builder's /home/user). Without this,\n# uid=0 target images silently get empty copy_artifacts.\nENV NPM_CONFIG_PREFIX={{.Home}}/.npm-global\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/package.json package.json\nRUN {{.CacheMountsOwned}}node -e 'var d=require(\"./package.json\").dependencies||{};for(var[n,v]of Object.entries(d))console.log(v===\"*\"?n:n+\"@\"+v)' | xargs npm install -g && rm -f package.json\n"

// aurStageTemplate is the verbatim former builder.aur.stage_template (cache-mount func →
// pre-rendered {{.CacheMountsAuto}}).
const aurStageTemplate = "FROM {{.BuilderRef}} AS {{.StageName}}\nUSER root\nRUN echo '{{.User}} ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/builder\nUSER {{.UID}}\nWORKDIR {{.Home}}\nENV XDG_CACHE_HOME=/tmp/aur-xdg-cache\nRUN {{.CacheMountsAuto}} \\\n    mkdir -p /tmp/aur-build /tmp/aur-srcdest /tmp/aur-xdg-cache && \\\n    cp /etc/makepkg.conf /tmp/makepkg.conf && \\\n    sed -i '/^OPTIONS/s/ debug/ !debug/' /tmp/makepkg.conf && \\\n    echo 'SRCDEST=/tmp/aur-srcdest' >> /tmp/makepkg.conf && \\\n    sudo pacman -Syu --noconfirm && \\\n    yay -S --noconfirm --needed --builddir /tmp/aur-build --makepkgconf /tmp/makepkg.conf\n{{- range .Options}} {{.}}{{end}}\n{{- range .Packages}} \\\n      {{.}}{{end}} && \\\n    mkdir -p /tmp/aur-pkgs && \\\n    find /tmp/aur-build -name '*.pkg.tar.zst' -exec cp {} /tmp/aur-pkgs/ \\; && \\\n    { ls /tmp/aur-pkgs/*.pkg.tar.zst >/dev/null 2>&1 || { echo 'charly aur builder: yay produced ZERO .pkg.tar.zst artifacts for a non-empty aur: package list — a package listed under aur: is likely now a repo package (yay -S --needed installs it from the repo without building), so nothing was copied for the main image to COPY. Move it to the package: list.' >&2; exit 1; }; }\n"

// CargoLibDir returns the directory a cargo LIBRARY crate's artifacts install into for a
// given candy. Per-candy rather than a flat /usr/local/lib so the install is identifiable and
// therefore reversible: BuilderReverse can remove exactly this directory, where a flat install
// would leave charly unable to tell its own .so files from anyone else's.
func CargoLibDir(candy string) string { return "/usr/local/lib/charly/" + candy }

// CargoLdConfPath returns the ld.so.conf.d drop-in that puts CargoLibDir on the loader path.
// Without it a per-candy directory would be invisible to the dynamic loader, which is the
// price of making the install identifiable — so the drop-in is emitted with it, never
// separately.
func CargoLdConfPath(candy string) string { return "/etc/ld.so.conf.d/charly-" + candy + ".conf" }

// cargoInlineTemplate is the former builder.cargo.install_template (cache-mount func →
// pre-rendered {{.CacheMountsOwned}}), extended with the LIBRARY branch. cargo is an INLINE
// builder — no separate FROM stage; this RUN emits IN the main image, returned as
// BuilderResolveReply.InlineFragment.
//
// `cargo install` installs BINARIES ONLY: on a crate that declares no `[[bin]]` it fails with
// "no binaries to install", so any candy shipping a cdylib GStreamer plugin, a staticlib, or a
// .so for LD_PRELOAD simply could not use this builder. The branch is in the TEMPLATE rather
// than in Go because the discriminator — the crate's own Cargo.toml — exists only inside the
// build context, and because a shell test needs no new schema, no new input field, and no
// authored `external_builder:`; detection stays detection.
//
// Cargo's binary detection is three rules, not one: an explicit [[bin]] section, src/main.rs,
// or any src/bin/*.rs. All three are tested, because treating a binary crate as a library
// would build it and then install nothing.
//
// The source is COPIED out of /ctx before EITHER branch runs, because a Containerfile
// `--mount=type=bind` is READ-ONLY and cargo writes into the crate directory:
//
//	cargo build   → "failed to write /ctx/Cargo.lock: Read-only file system"
//	cargo install → "Read-only file system (os error 30) at path /ctx/targetIg7IED"
//
// The second one is a PRE-EXISTING defect on the binary path, not something the library branch
// introduced: `cargo install --path` writes its intermediate artifacts beside the source. It
// went unnoticed because no production candy uses this builder — the only Cargo.toml beside a
// charly.yml in the whole corpus is charly's own cargo-tool test fixture — so the container
// cargo path had never actually run. Copying once, before the branch, fixes both.
//
// Both were found by RUNNING the rendered script against real crates in a container, not by
// reading it.
//
// A library crate that produces no cdylib/staticlib FAILS LOUDLY. The alternative — a glob
// that matches nothing and an install that quietly does nothing — is the exact silent no-op
// this builder already avoids on the aur path.
const cargoInlineTemplate = `RUN --mount=type=bind,from={{.LayerStage}},source=/,target=/ctx \
    {{.CacheMountsOwned}}set -e; \
    cp -a /ctx /tmp/charly-cargo-src; \
    if grep -qE '^[[:space:]]*\[\[bin\]\]' /tmp/charly-cargo-src/Cargo.toml || [ -f /tmp/charly-cargo-src/src/main.rs ] || ls /tmp/charly-cargo-src/src/bin/*.rs >/dev/null 2>&1; then \
      cargo install --path /tmp/charly-cargo-src; \
    else \
      cargo build --release --manifest-path /tmp/charly-cargo-src/Cargo.toml --target-dir /tmp/charly-cargo-target; \
      __n=0; \
      for __f in /tmp/charly-cargo-target/release/*.so /tmp/charly-cargo-target/release/*.a; do \
        [ -e "$__f" ] || continue; \
        install -Dm644 "$__f" '{{.LibDir}}'/"${__f##*/}"; \
        __n=$((__n+1)); \
      done; \
      [ "$__n" -gt 0 ] || { echo 'charly cargo builder: the crate declares no [[bin]], no src/main.rs and no src/bin/*.rs, so it was built as a LIBRARY — but cargo build --release produced no .so and no .a. Add crate-type = ["cdylib"] (or "staticlib") under [lib], or give the crate a binary target.' >&2; exit 1; }; \
      mkdir -p /etc/ld.so.conf.d && echo '{{.LibDir}}' > '{{.LdConf}}' && ldconfig; \
    fi
`

// BuilderResolve renders `word`'s build-time multi-stage from the host-supplied context,
// returning the pieces the host splices into the Containerfile: Stage (pre-main-FROM),
// CopyArtifacts + CopyBinary (post-main-FROM), or InlineFragment (in-candy, inline builders).
// An unknown word is a LOUD error (never a silent empty stage). This is the ONE render both
// the box-build plugin OpResolve and the in-proc pod-overlay build-emit call (R3).
func BuilderResolve(word string, in spec.BuilderResolveInput) (spec.BuilderResolveReply, error) {
	var zero spec.BuilderResolveReply
	switch word {
	case "pixi":
		stage, err := renderBuilderStage("pixi-stage", pixiStageTemplate, in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{
			Stage:         stage,
			CopyArtifacts: []string{builderCopyLine(in.StageName, in.Home, in.Home, true, in.UID, in.GID)},
			CopyBinary:    builderCopyLine(in.StageName, "/usr/local/bin/pixi", "/usr/local/bin/pixi", false, 0, 0),
		}, nil
	case "npm":
		stage, err := renderBuilderStage("npm-stage", npmStageTemplate, in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{
			Stage:         stage,
			CopyArtifacts: []string{builderCopyLine(in.StageName, in.Home, in.Home, true, in.UID, in.GID)},
		}, nil
	case "aur":
		stage, err := renderBuilderStage("aur-stage", aurStageTemplate, in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{
			Stage:         stage,
			CopyArtifacts: []string{builderCopyLine(in.StageName, "/tmp/aur-pkgs/", "/tmp/aur-pkgs/", false, 0, 0)},
		}, nil
	case "cargo":
		frag, err := renderCargoInline(in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{InlineFragment: frag}, nil
	}
	return zero, fmt.Errorf("kit.BuilderResolve: unknown detection-builder word %q", word)
}

// renderBuilderStage executes a relocated builder stage template against the resolve input.
// The templates use only stdlib text/template constructs (field access, if/range) — the
// cache-mount funcs were pre-rendered host-side — so no FuncMap is needed.
func renderBuilderStage(name, tmplStr string, in spec.BuilderResolveInput) (string, error) {
	return renderBuilderStageData(name, tmplStr, in)
}

// renderBuilderStageData is the one render (R3). It takes `any` so the cargo branch can pass a
// context that WRAPS the resolve input with derived per-candy paths, without every other
// builder growing fields it does not use.
func renderBuilderStageData(name, tmplStr string, data any) (string, error) {
	t, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse %s template: %w", name, err)
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("render %s template: %w", name, err)
	}
	return b.String(), nil
}

// builderCopyLine renders a `COPY --from=<stage> [--chown=uid:gid] <src> <dst>` directive —
// the exact shape the former emitBuilderArtifacts produced (copy_artifact / copy_binary),
// with no trailing newline (the host adds newlines when splicing).
func builderCopyLine(stage, src, dst string, chown bool, uid, gid int) string {
	if chown {
		return fmt.Sprintf("COPY --from=%s --chown=%d:%d %s %s", stage, uid, gid, src, dst)
	}
	return fmt.Sprintf("COPY --from=%s %s %s", stage, src, dst)
}

// cargoInlineData is the cargo template's render context: the resolve input plus the two
// per-candy paths the library branch installs into. They are derived here rather than added to
// spec.BuilderResolveInput because they are a FUNCTION of the candy name, not authored
// anywhere — putting them on the wire input would let a caller disagree with CargoLibDir.
type cargoInlineData struct {
	spec.BuilderResolveInput
	LibDir string
	LdConf string
}

func renderCargoInline(in spec.BuilderResolveInput) (string, error) {
	return renderBuilderStageData("cargo-inline", cargoInlineTemplate, cargoInlineData{
		BuilderResolveInput: in,
		LibDir:              CargoLibDir(in.Candy),
		LdConf:              CargoLdConfPath(in.Candy),
	})
}
