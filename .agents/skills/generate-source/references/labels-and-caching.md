## LABEL Placement — Cache Efficiency

**All OCI LABEL directives are emitted at the end of the final stage**,
after the last `USER` directive. This is an intentional cache-efficiency
choice driven by `sdk/deploykit`'s `WriteLabels` call being placed
after `WriteCandySteps` + the final `USER` emission (relocated from
`charly/generate.go` in #67).

Why it matters: the `ai.opencharly.description` LABEL is the most-volatile
piece of image metadata — it changes every time a scenario is added, edited,
or removed. If LABELs appeared BEFORE the RUN/COPY install steps,
buildkit's cache would invalidate at the first changed LABEL and every
downstream instruction would rebuild from scratch. For a 138-step stack
like `immich-ml`, that means re-running pnpm install, the Immich server
build, geodata downloads — minutes to hours for a one-line scenario edit.

With LABELs at the end, a scenario/label edit only re-runs the LABEL
instructions themselves (pure manifest metadata, no filesystem work).
Measured: ~2 seconds of delta over a no-change rebuild baseline of
~24 seconds for `filebrowser`.

LABELs are safe to move because they have no functional dependency on
subsequent instructions — they attach to the final image manifest and
are read only via `podman inspect` (an unordered map). None of the
runtime consumers (`charly config`, `charly start`, `charly status`, `charly check live`,
`charly shell`, `charly alias install`) care about directive order.

### LABEL JSON escaping (`writeJSONLabel`)

`writeJSONLabel` in `sdk/deploykit/write_labels.go` (relocated from `charly/generate.go` in #67) routes every LABEL value through
`spec.ShellQuote` before emitting `LABEL key=<quoted>`. This is
required because test/task commands often contain literal `'` characters
(e.g. `awk '{print $1}'`, `sed 's/foo/bar/'`) which JSON preserves
verbatim. Without escaping, the embedded quote terminates the LABEL's
own surrounding quote and the rest of the JSON blob is parsed as
`key=value` pairs — which typically fails with "can't find = in ..." on
the JSON payload.

### CalVer tag shifts do NOT invalidate cache

Each `charly box build` generate assigns a fresh CalVer timestamp and
emits it as the default of `ARG BASE_IMAGE=<registry>/<name>:<calver>`
at the top of every intermediate Containerfile. The ARG default
appears in the Containerfile text but is NOT part of the cache key
for subsequent RUN/COPY steps.

Concretely: podman/buildah resolves the `FROM ${BASE_IMAGE}` step
first — turning the tag into an image SHA — then keys every later
instruction off that SHA plus the instruction text plus COPY source
content. If the SHA is the same as a cached build's parent (because
the upstream image's content is unchanged), the whole rest of the
build cache-hits. A CalVer bump on an unchanged upstream is free.

The former `ai.opencharly.version` LABEL is **no longer emitted**: the
schema-versioning removal cutover deleted the authored `version:`, so
`sdk/deploykit/write_labels.go` drops the label and charly has no emitter left. The
surviving per-candy identity is `ai.opencharly.candy_version`, read from
`layer.GetVersion()` (`CandyModel.Version`), which the loader sets from the candy's
**git tag** (`sdk/loaderkit/scan_orchestrate.go` assigns `winner.GitTag`; the arbiter
is `sdk/loaderkit/candy_version.go`) — a source-derived value, STABLE across builds
when no layer changed, so it does not shift the image's config → SHA and therefore
does not cascade cache-misses to children via `FROM`. The local-image resolver still
READS `ai.opencharly.version` as its primary ordering key (`spec/container/
local_image_coneb.go`), but the label is absent on a locally-composed image, so
ordering falls through to the `:<calver>` build tag. `EffectiveVersion` survives
only as a documented, never-populated wire field (`schema/resolvedbox.cue`: the
highest candy git tag across the chain, else the base image's version).

Cache-miss only happens when something in the build input genuinely
changes: the parent image's content (different SHA resolved by the
FROM), a layer's scratch-stage content (`COPY candy/<name>/ /`
source bytes), or an instruction's text (package list, cmd body).
See `/charly-build:build` "Cache Efficiency" for the full list.

## OCI Labels

Built images embed runtime metadata as labels (prefix: `ai.opencharly.`), making images self-describing for runtime commands (`charly shell`, `charly start`, `charly config`, `charly alias install`).

| Label | Type | Example |
|-------|------|---------|
| `ai.opencharly.candy_version` | string | the candy's source-derived version — its GIT TAG (`sdk/loaderkit/scan_orchestrate.go`), e.g. `"2026.144.1443"` |
| `ai.opencharly.box` | string | `"openclaw"` |
| `ai.opencharly.registry` | string | `"ghcr.io/opencharly"` (omitted if empty) |
| `ai.opencharly.bootc` | string | `"true"` (omitted if false) |
| `ai.opencharly.uid` / `.gid` | string | `"1000"` |
| `ai.opencharly.user` / `.home` | string | `"user"` / `"/home/user"` |
| `ai.opencharly.port` | JSON | `["18789:18789"]` |
| `ai.opencharly.volume` | JSON | `[{"name":"data","path":"/home/user/.openclaw"}]` |
| `ai.opencharly.alias` | JSON | `[{"name":"openclaw","command":"openclaw"}]` |
| `ai.opencharly.security` | JSON | `{"privileged":false,"cap_add":["SYS_PTRACE"]}` |
| `ai.opencharly.network` | string | `"host"` (omitted if default) |
| `ai.opencharly.env` | JSON | `["KEY=VALUE"]` runtime env vars |
| `ai.opencharly.hook` | JSON | lifecycle hooks config |
| `ai.opencharly.route` | JSON | `[{"host":"app.localhost","port":8080}]` |
| `ai.opencharly.init` | string | active init system name |
| `ai.opencharly.init_def` | JSON | build-resolved init contract — `{entrypoint, fallback_entrypoint, management_tool, management_commands}` — read label-first at deploy for the entrypoint + in-container service-management surface |
| `ai.opencharly.service.<init>` | JSON | service names per init system |
| `ai.opencharly.env_candy` | JSON | candy-level env vars (merged) |
| `ai.opencharly.path_append` | JSON | PATH append entries |
| `ai.opencharly.platform.distro` | JSON | `["arch"]` distro identity (first match picks bootstrap/format templates) |
| `ai.opencharly.platform.format` | JSON | `["pac"]` package formats installed (`pac`, `rpm`, `deb`, `pixi`, `aur`, …) |
| `ai.opencharly.builder.use` | JSON | `{"aur":"arch-builder","pixi":"default-builder"}` consumer-side routing: format → builder image |
| `ai.opencharly.builder.provide` | JSON | `["pac","aur"]` producer-side capability: formats this image can build for others (builder images only) |
| `ai.opencharly.port_proto` | JSON | `{"9222":"tcp"}` port protocol overrides (non-http only) |
| `ai.opencharly.port_relay` | JSON | `[9222]` ports with socat relay |
| `ai.opencharly.status` | string | Effective status: `working`, `testing`, or `broken` (always emitted) |
| `ai.opencharly.info` | string | Aggregated status info from image + non-working layers (omitted if empty) |
| `ai.opencharly.candy_version` | JSON | `{"chrome":"2026.83.1430"}` candy name → CalVer (only versioned candies) |
| `ai.opencharly.skill` | string | Skill documentation URL (omitted if no skill exists) |

Tunnel configuration is NOT an OCI label — it is a deploy-time concern carried in `charly.yml` only.

Volumes use short names in labels (prefix `charly-<image>-` added at runtime). Empty arrays are omitted. JSON built from sorted slices for cache stability. Runtime commands read OCI labels exclusively (via `ExtractMetadata` in `spec/container/box_metadata_coneb.go`, re-exported as `kit.ExtractMetadata`/`deploykit.ExtractMetadata`) plus `charly.yml` overlay — they never touch `charly.yml` at runtime. That's why `charly shell myimage` works from any directory as long as the image is in local storage (if not, `ExtractMetadata` returns `spec.ErrImageNotLocal` and the CLI suggests `charly box pull`). See `/charly-image:image` for the build/deploy boundary and `/charly-build:pull` for the sentinel pattern. Labels also include `ai.opencharly.init` for init system identification and `ai.opencharly.service.<init>` for per-init service lists.

Source: `sdk/deploykit/write_labels.go` (`WriteLabels`, emission), `spec/container/box_metadata_coneb.go` (`ExtractMetadata`, read-back), `spec/spec/label_consts.go` (label names). `charly/labels.go` (the original home this behavior shipped in, and this doc's former source citation) no longer exists — relocated out of core; see `/charly-internals:capabilities`.

## Runtime-Only Features

Security configuration (`security:` in charly.yml) and environment variable injection (`env:`, `env_file:`) are **runtime-only** features. They affect container run arguments (`--privileged`, `--cap-add`, `-e`) but do not appear in generated Containerfiles.

## Cache Mounts

| Emission site | Cache path | Options |
|---|---|---|
| `rpm.packages` | `/var/cache/libdnf5` | `sharing=locked` |
| `deb.packages` | `/var/cache/apt` + `/var/lib/apt` | `sharing=locked` |
| `pac.packages` + `aur` | `/var/cache/pacman/pkg` | `sharing=locked` |
| `command:` as root | Distro-format caches (above) + `/ctx` bind to layer stage + any `cache:` (shared) | — |
| `command:` as non-root | `/var/cache/charly/npm-cache` (UID-scoped) + `/ctx` bind to layer stage + any `cache:` (owned) | `uid=<UID>,gid=<GID>` |
| `download:` | `/var/cache/charly/downloads` (shared) — **content-addressed**: file fetched once, reused across builds + any `cache:` | — |
| `task: cache:` | each declared path; ownership from the task `run_as:` (root → shared/locked, else uid/gid-owned) | per-user |
| AUR builder stage | `/var/cache/pacman/pkg` (shared) + `/tmp/aur-srcdest` + `/tmp/aur-xdg-cache` (owned: makepkg SRCDEST + yay clones) | mixed |
| pixi builder stage | `/tmp/pixi-cache` + `/tmp/rattler-cache` | `uid=<UID>,gid=<GID>` |
| npm builder stage | `/var/cache/charly/npm-cache` | `uid=<UID>,gid=<GID>` |
| cargo inline | `/tmp/cargo-cache` | `uid=<UID>,gid=<GID>` |

UID/GID in cache mounts are dynamic (from resolved image config). Ownership is what avoids the buildah permission problem: a non-root cache mount is uid/gid-OWNED (`OwnedCacheMount`, a per-uid id namespace) rather than shared root-owned. The vocabulary stage caches (`EmitCmd`/builder stages — pixi/rattler/aur/cargo) keep their flat `/tmp/<tool>-cache` roots, while the two emitter-level caches moved to the nested `/var/cache/charly/{downloads,npm-cache}` (sdk#279) — both owned for a non-root stage (`buildkit.OwnedCacheMount`), a shared/locked mount only for a root stage. `deploykit/tasks_emit.go`: `buildCacheRoot = "/var/cache/charly"` + `downloadsCacheDir`/`npmCacheDir` under it.

### Download caching + the generic `cache:` modifier (`EmitDownload` / `TaskCacheMounts`)

`EmitDownload` (`sdk/deploykit/tasks_emit.go`) writes every `download:` to a **content-addressed**
file in the `/var/cache/charly/downloads` cache mount: `__c=/var/cache/charly/downloads/$(printf %s "$url"
| sha256sum | cut -c1-64)`, fetched only when absent (`[ -s "$__c" ] ||`), via
`curl -o "$__c.part" && mv -f "$__c.part" "$__c"` (atomic rename — a
partial/corrupt download is never reused), then the extractor reads `"$__c"`.
So a file fetched once survives an upstream layer cache-miss instead of
re-downloading (the prior code declared the mount but streamed curl past it).

`TaskCacheMounts(t, img)` (`sdk/deploykit/tasks_emit.go`) renders a task's layer-declared
`cache:` paths as cache-mount flags, ownership derived from the task's `run_as:`
via `ResolveUserSpec` (root → `SharedCacheMount`, else → `OwnedCacheMount`).
Honored by `EmitCmd` and `EmitDownload`. Lets any task persist heavy
downloads/build artifacts the same way package caches do (config-driven; the
cache-USE logic lives in the charly.yml task body).

`CacheMountDef.Owned` + `RenderCacheMountsAuto` (`cacheMountsAuto` template
func) let a single builder `cache_mount` list mix shared (root, e.g. pacman) and
owned (user, e.g. makepkg SRCDEST / yay clones) entries — used by the `aur`
builder in the embedded build vocabulary to cache AUR source downloads + clones across builds.

