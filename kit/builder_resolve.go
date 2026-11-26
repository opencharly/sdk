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
// Selection stays host-side (deploykit.NeedsBuilder: a detect file, or a manager declaration the
// image's tag chain selects); the host computes the full BuilderResolveInput (builder ref, stage
// name, target identity, filesystem-detected manifest/lockfile/build-script, manager
// packages/options, pre-rendered cache mounts) and
// Invokes the plugin's OpResolve; the plugin
// returns the rendered Stage + CopyArtifacts (+ CopyBinary / InlineFragment / Package) via THIS
// function; the payload and the package output are the plugin's own knowledge (builderPayloadCopies,
// BuilderPackageOutput), never the authored builder node.

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/opencharly/spec/spec"
)

// pixiStageTemplate is the verbatim former builder.pixi.stage_template (cache-mount func →
// pre-rendered {{.CacheMountsOwned}}).
const pixiStageTemplate = "FROM {{.BuilderRef}} AS {{.StageName}}\nUSER {{.UID}}\nWORKDIR {{.Home}}\n# The ENV pins are load-bearing, exactly like npmStageTemplate's NPM_CONFIG_PREFIX: pixi\n# installs its environment under $PIXI_HOME (default $HOME/.pixi), NOT the project dir, and\n# USER+WORKDIR alone do NOT set HOME — it comes from the BASE image's passwd for that uid.\n# When a rebuilt base moves uid 1000's home, the env lands outside {{.Home}} and the final\n# stage's `COPY --from=<stage> {{.Home}} {{.Home}}` carries no .pixi (measured: every image\n# check that reads {{.Home}}/.pixi/envs/default/bin/python went red).\nENV HOME={{.Home}} PIXI_HOME={{.Home}}/.pixi\n{{- if .HasLockFile}}\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/pixi.lock pixi.lock\n{{- end}}\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/{{.Manifest}} {{.Manifest}}\n{{.ManylinuxFix}}\nENV PIXI_CACHE_DIR=/tmp/pixi-cache\n# RATTLER_CACHE_DIR is deliberately NOT set. Measured on pixi 0.70.2 AND 0.84.2 with a\n# `--frozen` install: the package cache lands in PIXI_CACHE_DIR (`pkgs`, `uv-cache`) and\n# RATTLER_CACHE_DIR is never created, so setting it only declared a cache root nothing\n# writes. The repodata cache is absent by design under `--frozen` (the lock is used, nothing\n# is solved), so it is not a cache miss either.\n{{- if .HasBuildScript}}\n# build.sh is COPY'd (NOT bind-mounted) so its CONTENT is part of this\n# stage's BuildKit cache key — editing build.sh (e.g. the pixelflux\n# NVENC patch) MUST invalidate the compile. A\n# `--mount=type=bind,from=<stage>,source=/build.sh` delivers the file\n# but its content NEVER enters the RUN cache key (the key is parent-SHA\n# + COPY'd manifest + RUN-text), so a changed build.sh silently reused\n# a stale compiled artifact — the \"new code not picked up\" bug. COPY\n# keys it exactly like pixi.toml / pixi.lock above.\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/{{.BuildScript}} /tmp/{{.BuildScript}}\nRUN {{.CacheMountsOwned}}{{.InstallCmd}} && test -d {{.Home}}/.pixi && bash /tmp/{{.BuildScript}} && rm -f {{.Manifest}} pixi.lock\n{{- else}}\nRUN {{.CacheMountsOwned}}{{.InstallCmd}} && test -d {{.Home}}/.pixi && rm -f {{.Manifest}} pixi.lock\n{{- end}}\n"

// npmStageTemplate is the verbatim former builder.npm.stage_template.
const npmStageTemplate = "FROM {{.BuilderRef}} AS {{.StageName}}\nUSER {{.UID}}\nWORKDIR {{.Home}}\n# Override NPM_CONFIG_PREFIX from the builder image so npm writes to\n# the TARGET image's HOME (not the builder's /home/user). Without this,\n# uid=0 target images silently get an empty payload.\nENV NPM_CONFIG_PREFIX={{.Home}}/.npm-global\nCOPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/package.json package.json\nRUN {{.CacheMountsOwned}}node -e 'var d=require(\"./package.json\").dependencies||{};for(var[n,v]of Object.entries(d))console.log(v===\"*\"?n:n+\"@\"+v)' | xargs npm install -g && rm -f package.json\n"

// aurStageTemplate is the verbatim former builder.aur.stage_template (cache-mount func →
// pre-rendered {{.CacheMountsAuto}}).
const aurStageTemplate = "FROM {{.BuilderRef}} AS {{.StageName}}\nUSER root\nRUN echo '{{.User}} ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/builder\nUSER {{.UID}}\nWORKDIR {{.Home}}\nENV XDG_CACHE_HOME=/tmp/aur-xdg-cache\nRUN {{.CacheMountsAuto}} \\\n    mkdir -p /tmp/aur-build /tmp/aur-srcdest /tmp/aur-xdg-cache && \\\n    cp /etc/makepkg.conf /tmp/makepkg.conf && \\\n    sed -i '/^OPTIONS/s/ debug/ !debug/' /tmp/makepkg.conf && \\\n    echo 'SRCDEST=/tmp/aur-srcdest' >> /tmp/makepkg.conf && \\\n    sudo pacman -Syu --noconfirm && \\\n    yay -S --noconfirm --needed --builddir /tmp/aur-build --makepkgconf /tmp/makepkg.conf\n{{- range .Options}} {{.}}{{end}}\n{{- range .Packages}} \\\n      {{.}}{{end}} && \\\n    mkdir -p {{.PkgDir}} {{.PayloadDir}} && \\\n    find /tmp/aur-build -name '*.pkg.tar.zst' -exec cp {} {{.PkgDir}}/ \\; && \\\n    { ls {{.PkgDir}}/*.pkg.tar.zst >/dev/null 2>&1 || { echo 'charly aur builder: yay produced ZERO .pkg.tar.zst artifacts for a non-empty aur: package list — a package listed under aur: is likely now a repo package (yay -S --needed installs it from the repo without building), so nothing was copied for the main image to COPY. Move it to the package: list.' >&2; exit 1; }; } && \\\n    for p in {{.PkgDir}}/*.pkg.tar.zst; do bsdtar -xpf \"$p\" -C {{.PayloadDir}} --exclude .PKGINFO --exclude .BUILDINFO --exclude .MTREE --exclude .INSTALL --exclude .CHANGELOG; done\n"

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

// miseVersion is the pinned mise release the builder stage installs. Bump
// deliberately (mise releases weekly); the tarball naming is Go-style arch
// (x64/arm64) with a -musl suffix for musl libc bases (Alpine), mapped from
// uname + the musl-loader probe at build time.
const miseVersion = "v2026.8.14"

// miseStageTemplate is the builder.mise stage. The stage is DISTRO-AGNOSTIC: its FROM is
// the host-computed BuilderRef (the image's builder box — distro-appropriate, e.g.
// fedora-builder or cachyos-builder), the toolchain (curl+tar) comes from the builder
// image, and the install command comes from the embedded builder: mise: vocabulary's
// install_command ({{.InstallCmd}}). The only fixed paths are mise's OWN conventions:
// the binary lands at /usr/local/bin/mise (the release tarball's layout) and the data
// dir is mise's default $HOME/.local/share/mise (the CopyArtifacts copy the stage's
// Home, exactly like the pixi builder's ~/.pixi). Distro knowledge stays in the host's
// vocabulary — a builder template never names a distro. The release tarball is
// libc-specific: mise publishes glibc builds (mise-<ver>-linux-<arch>.tar.gz) AND musl
// builds (mise-<ver>-linux-<arch>-musl.tar.gz); the stage probes for the musl loader
// (/lib/ld-musl-*.so.1) and appends the -musl suffix on musl bases, so the glibc binary
// never lands on a musl image where it cannot exec (missing /lib64/ld-linux-*).
const miseStageTemplate = `FROM {{.BuilderRef}} AS {{.StageName}}
USER root
RUN A=$(uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/') && \
    M=""; if [ -e /lib/ld-musl-x86_64.so.1 ] || [ -e /lib/ld-musl-aarch64.so.1 ]; then M="-musl"; fi && \
    curl -sL -o /tmp/mise.tar.gz https://github.com/jdx/mise/releases/download/{{.MiseVersion}}/mise-{{.MiseVersion}}-linux-$A$M.tar.gz && \
    tar xzf /tmp/mise.tar.gz -C /usr/local --strip-components=1 && rm /tmp/mise.tar.gz
USER {{.UID}}
WORKDIR {{.Home}}
ENV MISE_YES=1
COPY --chown={{.UID}}:{{.GID}} {{.CopySrc}}/{{.Manifest}} {{.Manifest}}
RUN {{.CacheMountsOwned}}{{.InstallCmd}} && mise reshim
`

// The builder reply's REQUIRED `output` discriminator (spec schema, DESIGN §4.4): a reply that
// does not name its shape is invalid at receipt, so a plugin built against the older contract
// fails at its first message instead of shipping a payload nothing installs
// (opencharly/sdk#615). The host checks the pair (deploykit.ValidateBuilderResolveReply).
const (
	BuilderOutputPackage = "package"
	BuilderOutputFiles   = "files"
	BuilderOutputInline  = "inline"
)

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
		copies, err := builderPayloadCopies(word, in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{
			Output:        BuilderOutputFiles,
			Stage:         stage,
			CopyArtifacts: copies,
			CopyBinary:    builderCopyLine(in.StageName, "/usr/local/bin/pixi", "/usr/local/bin/pixi", false, 0, 0),
		}, nil
	case "npm":
		stage, err := renderBuilderStage("npm-stage", npmStageTemplate, in)
		if err != nil {
			return zero, err
		}
		copies, err := builderPayloadCopies(word, in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{Output: BuilderOutputFiles, Stage: stage, CopyArtifacts: copies}, nil
	case "aur":
		pkg := BuilderPackageOutput(word, in.Candy)
		if pkg == nil {
			return zero, fmt.Errorf("aur builder, candy %q: no package output is defined for aur", in.Candy)
		}
		stage, err := renderBuilderStageData("aur-stage", aurStageTemplate, aurStageData{BuilderResolveInput: in, PkgDir: pkg.Dir, PayloadDir: AurPayloadDir(in.Candy)})
		if err != nil {
			return zero, err
		}
		copies, err := builderPayloadCopies(word, in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{Output: BuilderOutputPackage, Stage: stage, CopyArtifacts: copies, Package: pkg}, nil
	case "cargo":
		frag, err := renderCargoInline(in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{Output: BuilderOutputInline, InlineFragment: frag}, nil
	case "mise":
		stage, err := renderBuilderStageData("mise-stage", miseStageTemplate, miseStageData{BuilderResolveInput: in, MiseVersion: miseVersion})
		if err != nil {
			return zero, err
		}
		copies, err := builderPayloadCopies(word, in)
		if err != nil {
			return zero, err
		}
		return spec.BuilderResolveReply{
			Output:        BuilderOutputFiles,
			Stage:         stage,
			CopyArtifacts: copies,
			CopyBinary:    builderCopyLine(in.StageName, "/usr/local/bin/mise", "/usr/local/bin/mise", false, 0, 0),
		}, nil
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

// builderCopyLine renders a `COPY --from=<stage> [--chown=uid:gid] <src> <dst>` directive,
// with no trailing newline (the host adds newlines when splicing).
func builderCopyLine(stage, src, dst string, chown bool, uid, gid int) string {
	if chown {
		return fmt.Sprintf("COPY --from=%s --chown=%d:%d %s %s", stage, uid, gid, src, dst)
	}
	return fmt.Sprintf("COPY --from=%s %s %s", stage, src, dst)
}

// builderPayloadCopies is each stage builder's payload COPY into the main image — the builder
// plugin's OWN knowledge (DESIGN.cue #BuilderAlt: the authored builder node is only a manager map,
// and what a builder installs is the plugin's, P3), defined once here for every stage builder: the
// user-home builders (pixi/npm/mise) copy the stage's home, and aur copies its extracted package
// tree. A stage word with no payload here is an error, never a stage whose output reaches no image
// (opencharly/sdk#608).
func builderPayloadCopies(word string, in spec.BuilderResolveInput) ([]string, error) {
	switch word {
	case "pixi", "npm", "mise":
		return []string{builderCopyLine(in.StageName, in.Home, in.Home, true, in.UID, in.GID)}, nil
	case "aur":
		return []string{builderCopyLine(in.StageName, AurPayloadDir(in.Candy)+"/", "/", false, 0, 0)}, nil
	}
	return nil, fmt.Errorf("builder %q, candy %q: no payload is defined for this stage builder", word, in.Candy)
}

// BuilderPackageOutput is the OS package output `word` produces for `candy` — the builder plugin's
// own declaration, returned in its resolve and collect replies (#BuilderPackage): aur builds pac
// packages into the candy's OWN directory, so two candies' packages never share one. Nil for every
// builder whose output is a payload of files.
func BuilderPackageOutput(word, candy string) *spec.BuilderPackage {
	if word == "aur" {
		return &spec.BuilderPackage{Format: "pac", Dir: "/tmp/aur-pkgs/" + candy, Glob: "*.pkg.tar.zst"}
	}
	return nil
}

// AurPayloadDir is where the aur stage extracts a candy's built packages as a file tree — the
// payload a target WITHOUT a pacman local_pkg contract receives (builderPayloadCopies copies it to
// the image root). A function of the candy name, like CargoLibDir.
func AurPayloadDir(candy string) string { return "/tmp/aur-root/" + candy }

// aurStageData is the aur template's render context: the resolve input plus the candy's package
// and payload directories.
type aurStageData struct {
	spec.BuilderResolveInput
	PkgDir     string
	PayloadDir string
}

// miseStageData is the mise template's render context: the resolve input plus the
// pinned mise release version (named, not a magic literal in the template).
type miseStageData struct {
	spec.BuilderResolveInput
	MiseVersion string
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
