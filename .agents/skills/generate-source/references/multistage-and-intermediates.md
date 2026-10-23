## Multi-Stage Builds

### Pixi Build Stage

```dockerfile
FROM ghcr.io/opencharly/fedora-builder:2026.48.1808 AS supervisord-pixi-build
WORKDIR /home/user
COPY candy/supervisord/pixi.toml pixi.toml
RUN pixi install
```

Uses the configured builder image. No `apt-get install` needed — builder has pixi, node, npm, gcc, cmake, git.

### npm Build Stage

```dockerfile
FROM <builder> AS openclaw-npm-build
COPY candy/openclaw/package.json package.json
RUN npm install -g --prefix /npm-global
```

### AUR Build Stage (Arch Linux)

For layers with `aur:` packages, the generator creates a multi-stage build using the `builders.aur` image:

```dockerfile
FROM ghcr.io/opencharly/arch-builder:2026.84.942 AS my-tool-aur-build
USER root
RUN echo 'user ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/builder
USER 1000
WORKDIR /home/user
RUN --mount=type=cache,dst=/var/cache/pacman/pkg,sharing=locked \
    mkdir -p /tmp/aur-build && \
    cp /etc/makepkg.conf /tmp/makepkg.conf && \
    sed -i '/^OPTIONS/s/ debug/ !debug/' /tmp/makepkg.conf && \
    yay -S --noconfirm --needed --builddir /tmp/aur-build --makepkgconf /tmp/makepkg.conf \
      aur-package && \
    mkdir -p /tmp/aur-pkgs && \
    find /tmp/aur-build -name '*.pkg.tar.zst' -exec cp {} /tmp/aur-pkgs/ \;
```

In the main image, the built packages are installed:
```dockerfile
COPY --from=my-tool-aur-build /tmp/aur-pkgs/ /tmp/aur-pkgs/
RUN --mount=type=cache,dst=/var/cache/pacman/pkg,sharing=locked \
    pacman -U --noconfirm /tmp/aur-pkgs/*.pkg.tar.zst && \
    rm -rf /tmp/aur-pkgs
```

Key details: passwordless sudo is required because yay calls `pacman -U` as root. Debug packages are disabled via a patched copy of `makepkg.conf` (the build user can't modify `/etc/` directly).

**Status:** Working. Verified with `yay-bin` in `arch-test` image.

### Builder stages via OpResolve (the build-time BUILDER leg)

The pixi/npm/aur stages above are NOT rendered from an in-core `builders.<name>` vocabulary. Since C10 **both** the four DETECTION-builders (pixi/npm/cargo/aur — selected by a candy's `pixi.toml` / `package.json` / `Cargo.toml` / `aur:` section) **and** an OUT-OF-TREE builder selected by a candy's `external_builder: <word>` field emit their build-time multi-stage through the SAME `OpResolve` seam: the plugin returns a `spec.BuilderResolveReply` the generator splices. The stage TEMPLATES for the four detection-builders live in the shared `sdk/kit.BuilderResolve` (imported by each `candy/plugin-builder-<word>`); the former embedded vocabulary's `stage_template`/`copy_artifact`/`copy_binary`/cargo `install_template` are gone.

- **`EmitBuilderStages`** (PRE-main-FROM, deploykit) — for each candy a builder DETECTS (`candyNeedsBuilder`), connects the externalized builder plugin on-demand (`ensureBuildersConnected` — the SAME scoped connect the deploy build pre-pass uses, R2) and `Invoke(OpResolve)`s it (the shared `renderSeamCaller.resolveBuilderStage`, `sdk/deploykit/render_generator_from_project.go` — the former `charly/generate.go` is DELETED, K-wave 2), writing the returned `Stage` (a `FROM <ref> AS <name>` block) verbatim and caching the reply per (candy, builder) on the `Generator`. The HOST computes the render context (builder ref, stage name, copy src, uid/gid/home, filesystem-detected manifest/lockfile/build-script, aur packages/options, PRE-RENDERED cache-mount flag strings) into a `spec.BuilderResolveInput`; the plugin (kit.BuilderResolve) owns the stage template.
- **`EmitBuilderArtifacts`** (POST-main-FROM, deploykit) — writes the cached reply's `CopyArtifacts` (`COPY --from=<stage> …`) + the once-per-builder `CopyBinary` (pixi → `/usr/local/bin/pixi`), deduped across candies.
- An INLINE detection builder (cargo) has no separate FROM stage — its OpResolve reply carries an `InlineFragment` (the in-candy `RUN … cargo install --path /ctx`) that `WriteCandySteps` (deploykit) splices in the candy's step sequence.
- **`EmitExternalBuilderStages` / `EmitExternalBuilderArtifacts`** (the `external_builder:` path, deploykit) run right after their detection counterparts and share `resolveBuilderStage`: a candy sets `external_builder: <word>`, the word resolves to an EXTERNAL `*grpcProvider`, and its OpResolve reply's `Stage`/`CopyArtifacts` splice the same way (it sends a MINIMAL input — candy name only — since an out-of-tree builder renders a self-contained stage).

The pod-overlay build-emit for `builder` (C1.3) renders the detection builders through the SAME `kit.BuilderResolve`, now from the plugin's own `deploykit.Generator` (the fetched `"resolved-project"` envelope, K5-Unit-6b) rather than the former in-proc host `stepEmitBuilder` (GONE), so box-build and pod-overlay share ONE render. An empty `Stage` (or, for cargo, an empty `InlineFragment`), an unresolvable word, or an Invoke error fails LOUDLY (R3) — never a silently-dropped builder stage. Both halves are egress-validated with the rest of the Containerfile before write. See `/charly-build:generate` + `/charly-internals:plugin` (placement).

### Scratch Context Stage

```dockerfile
FROM scratch AS mylib-ctx
COPY candy/mylib/ /
```

## Auto-Intermediate Images — grouping by `(Base, UID)`

Sibling images that share the same base are grouped by
`ComputeIntermediates` in `sdk/deploykit/intermediates.go` (moved from `charly/intermediates.go`; `charly/intermediates_shim.go` delegates to it) so repeated layer
prefixes become shared intermediate images (the `fedora-nonfree-ssh-client-dbus`
pattern). The grouping key is `siblingKey{base, uid}`, not a bare base
string: grouping on base alone would collapse images with **different
UIDs** into one intermediate whose resolved `HOME` is always the
default (`/home/user`), so a child image with `uid: 0` (`/home/root`)
would inherit `/home/user/.pixi/bin` as dead `ENV PATH` entries it
can't override.

Per-UID sibling groups get their own intermediate chains. When the UID
differs from `cfg.Defaults.UID` (typically 1000), `pickAutoName`
appends `-uid<N>` to the intermediate name so OCI tags don't collide
with the default-UID variant.

```go
// sdk/deploykit/intermediates.go (abridged)
type siblingKey struct {
    base string
    uid  int
}
siblingGroups := make(map[siblingKey][]string)
for name, img := range images {
    if builderNames[name] {
        continue
    }
    k := siblingKey{img.Base, img.UID}
    siblingGroups[k] = append(siblingGroups[k], name)
}
```

`createIntermediate()` then derives `User/GID/Home` from the group's
UID (uid=0 → `root`/`/root`; else default user + `/home/<user>`)
instead of always using defaults. This keeps HOME-relative
`env:` / `path_append:` expansion consistent with the child images.

Blast radius: uid=1000 images (the overwhelming majority) share their
intermediates as usual. Uid=0 images get their own `-uid0`-suffixed
intermediates. The ecosystem's coder/charly images run as uid=1000 + sudo
(see `/charly-coder:fedora-coder`), so the per-UID split is dormant
protection against any future uid=0 image — it stays in place because
the defensive grouping is cheap (one extra struct field) and prevents
a whole class of silent PATH regressions.

Covered by `sdk/deploykit/intermediates_move_test.go` (the test images construct
`ResolvedBox{}` without an explicit UID, so they trip `uid=0` →
`-uid0` naming; the former `charly/intermediates_test.go` is DELETED, K-wave 2).

## Intermediate Images

Auto-generated at branch points where multiple images share common layer prefixes:

```
fedora (external)
  -> fedora-supervisord (auto: pixi + python + supervisord)
     -> fedora-test (adds: traefik, testapi)
     -> openclaw (adds: nodejs, openclaw)
```

`ComputeIntermediates()` uses a prefix trie to detect divergence points. `GlobalCandyOrder()` prioritizes popular layers for cache efficiency.

### Parent-vs-defaults inheritance (critical)

`createIntermediate()` must inherit `Distro` and `BuildFormats` **from the parent image first**, with `cfg.Defaults.*` as the fallback only when the parent is external or empty. The inverse — defaults winning over an explicit parent — is a silent-failure bug: an `arch`-rooted auto-intermediate would get re-tagged as `build: [rpm]` (because `defaults.Build=[rpm]` in `charly.yml`), then the layer emitter would scan each layer for an `rpm:` section and find nothing (the layers only declare `pac:`) — emitting empty RUN steps. Symptom: the intermediate built fine but shipped without any of its layers' packages (e.g. `arch-ssh-client` without direnv/gnupg/openssh).

The current code resolves `inheritedDistro` / `inheritedBuilds` from the parent first and only falls back to defaults when both are empty. Regression guard: `TestComputeIntermediates_InheritDistroFromParent` constructs a config with `defaults.Build=[rpm]` but an `arch` parent carrying `[pac]`, and asserts that the auto-intermediate comes out `[pac]`. See `/charly-internals:go` `intermediates.go` row for the file-level note.

## User Resolution

Configurable via `user`, `uid`, `gid`, and `user_policy` fields in `charly.yml` (defaults: `"user"`, 1000, 1000, `"auto"`).

### Declarative adopt: the embedded `distro.<name>.base_user:`

Some base images ship a pre-existing uid-1000 account (Ubuntu 24.04 ships `ubuntu:ubuntu`). The `base_user:` declaration in the embedded build vocabulary tells charly about it:

```yaml
distro:
  ubuntu:
    base_user: { name: ubuntu, uid: 1000, gid: 1000, home: /home/ubuntu }
```

`sdk/buildkit/config_resolve.go:ResolveBox` reconciles this against the image's `user_policy:`:

- `auto` (default) — adopt `base_user` when declared AND image didn't explicitly set `user:`.
- `adopt` — always adopt; hard-errors without a declaration.
- `create` — always create via useradd.

When adopt fires, `resolved.User` / `UID` / `GID` / `Home` are overwritten and `resolved.UserAdopted = true`.

### WriteBootstrap — two emission branches

`sdk/deploykit` `WriteBootstrap` (relocated from `charly/generate.go` in #67) keys on `img.UserAdopted`:

**Adopt (no useradd):**
```dockerfile
# User ubuntu (uid=1000) adopted from base image (declared in the embedded distro.base_user vocabulary) — no useradd needed

WORKDIR /home/ubuntu
USER 1000
```

**Create (idempotent useradd):**
```dockerfile
RUN if ! getent passwd <UID> >/dev/null 2>&1; then \
      LOGIN_SHELL=/bin/sh; [ -x /bin/bash ] && LOGIN_SHELL=/bin/bash; \
      if command -v useradd >/dev/null 2>&1; then \
        (getent group <GID> >/dev/null 2>&1 || groupadd -g <GID> <user>) && \
        useradd -m -u <UID> -g <GID> -s "$LOGIN_SHELL" <user>; \
      else \
        (getent group <GID> >/dev/null 2>&1 || addgroup -g <GID> <user>) && \
        adduser -D -h <home> -u <UID> -G <user> -s "$LOGIN_SHELL" <user>; \
      fi; \
    fi

WORKDIR /home/<user>
USER <UID>
```

The create branch uses the `if !` form (rather than a `getent passwd <UID> >/dev/null 2>&1 ||` short-circuit) because it composes better with the adopt branch — no risk of silently short-circuiting on an unexpectedly-pre-existing user.

### Fallback path: `registry.go:InspectImageUser`

Pulls the base image via go-containerregistry and extracts `/etc/passwd` to auto-detect the home dir. The declarative `base_user:` approach is preferred (simpler, no network round-trip, explicit intent); `InspectImageUser` remains as a fallback for bases without a `base_user:` declaration.

Source: `charly/config.go:ResolveBox` (policy reconciliation), `sdk/deploykit/bootstrap.go:WriteBootstrap` (emission, relocated in #67), `charly/format_config.go:BaseUserDef` (struct), `charly/registry.go:InspectImageUser` (fallback).

