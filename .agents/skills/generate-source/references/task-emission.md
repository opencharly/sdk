## Task emission pipeline

All install-task logic lives in `sdk/deploykit` (`tasks_emit.go` + `tasks_render.go`); `charly/tasks.go` is 64 lines holding ONE function, `invokeOpEmitFragmentOpt`, and cannot call into deploykit at all (P16 import purity) (the actual per-verb emitters, relocated from `charly/generate.go` in #67). Authored layer-side as `task:` list in `charly.yml`; emitted as Containerfile directives via this sequence per layer inside `WriteCandySteps` (`sdk/deploykit/candy_steps.go`):

```
1. # Layer: <name>                 (comment header)
2. ARG TARGETARCH + ENV ARCH=${TARGETARCH}    (once; from EmitVarsEnv)
3. ENV <K>=<V> for each layer.Vars entry      (also from EmitVarsEnv)
4. Package install: rpm/deb/pac/aur/apk  (unchanged — format template render)
5. (g *Generator) EmitTasks(b, layer, img, ops []vmshared.Op, buildDir, contextRelPrefix) (string, error):
   for each t in layer.tasks:
     resolve ${VAR} in non-verbatim fields
     user := ResolveUserSpec(t.User, img)   // numeric UID for ${USER}
     if user != runningUser: emit USER <value>
     switch t.Kind():
       case "mkdir":    EmitMkdirBatch  (coalesces adjacent same-user+same-mode)
       case "copy":     EmitCopy        (COPY --from=<layer> --chmod= --chown=)
       case "write":    StageInlineContent + EmitWrite (COPY from .build _inline)
       case "link":     EmitLinkBatch   (coalesces adjacent same-user)
       case "download": EmitDownload    (RUN curl + extractor + /var/cache/charly/downloads cache)
       case "setcap":   EmitSetcapBatch (coalesces; strip on empty caps)
       case "command":  EmitCmd         (RUN <shell-probe> <<'OVCMD' … OVCMD + /ctx bind)
       case "build":    WriteCandySteps (deploykit) handles builder placement
       case "plugin":   builtin ProvisionActor → act shell RUN (in-proc);
                        any other provider → invokeOpEmitFragmentOpt → Invoke(OpEmit)
                        → spec.EmitReply.Fragment spliced verbatim
6. USER root reset (unless last layer + skipRootReset)
```

The `plugin:` verb case is placement-agnostic above the registry. `plugin: command` is the one special case — it rehydrates `plugin_input.command` and emits via the SAME `EmitCmd` as the literal `command` verb. Every other `plugin:` step resolves its provider via `providerRegistry.ResolveVerb`: a builtin `ProvisionActor` renders an act shell `RUN` in-proc (the zero-JSON fast path, `resolveProvisionScript`), while ANY other resolved provider — an external `grpcProvider` (host-built + connected by the build-time plugin connect seam `loadProjectPlugins` (`charly/host_build_buildengine.go:51`); the former host-side `NewGenerator` is DELETED), or a builtin emitting a richer fragment — renders via `invokeOpEmitFragmentOpt`, which calls `prov.Invoke(OpEmit)` with the step's `plugin_input` (`op.Params`) + a `spec.BuildEnv` descriptor (`op.Env`) and splices the returned `spec.EmitReply.Fragment` verbatim into the Containerfile. An unresolved verb is a loud error, never a silently-dropped step. This is the build half of operator-authorized build-time plugin execution — see `/charly-internals:plugin` (placement) + `/charly-build:generate`. (`invokeOpEmitFragmentOpt`'s core is factored into the shared `invokeOpEmitFragment` seam, which the DEPLOY-mode pod-overlay `dispatchOCIStep` external-step arm (reached by the candy's `deploykit.OCITarget.EmitStepOp` over `HostBuild("step-emit","oci-emit-step")`) ALSO uses to bake an `external:<word>` `class:step` step declaring `StepContract.Emits=true` — F-STEP-EMIT, see `/charly-internals:install-plan`.)

### `Task` struct (`charly/layers.go:604`)

Flat struct with verb-discriminator fields. Exactly one of `Cmd` / `Mkdir` / `Copy` / `Write` / `Link` / `Download` / `Setcap` / `Build` must be non-empty. Shared modifiers (`User`, `Mode`, `To`, `Target`, `Content`, `Extract`, `Include`, `Env`, `Caps`, `Comment`) are validated per-verb in `candy/plugin-box/validate_rules.go:validateCandyTasks`.

```go
func (t *Task) Kind() (string, error)   // returns verb or error ("no action" / "conflicting actions")
```

The `Layer` struct that feeds emission carries its `require:` / `candy:` refs as `[]CandyRef` (one typed list each — no parallel bare/raw arrays), and its `Has*` predicates (`HasEnv`/`HasPorts`/`HasVolumes`/…) are derived methods (only the filesystem-probe caches like `HasPixiToml` stay fields). Layers are populated by the single `scanFromParsed` (`sdk/loaderkit/scan_candy.go`). Which layers reach this emission pipeline is decided upstream by the reachability-scoped, per-entity-version resolver (the git tag is the fetch coordinate; the layer's own `version:` is the identity) — see `/charly-internals:go` "Remote-layer resolver".

### Emitter helpers (all in `sdk/deploykit/tasks_emit.go`)

| Helper | Output |
|---|---|
| `EmitVarsEnv(b, vars)` | `ARG TARGETARCH` + `ENV ARCH=${TARGETARCH}` + sorted `ENV K=V` |
| `EmitMkdirBatch(b, []Op, img)` | One `RUN mkdir -p … [&& chmod <mode> …]` |
| `EmitCopy(b, Op, layerStage, img)` | `COPY --from=<layerStage> --chmod= [--chown=] <src> <to>` |
| `EmitWrite(b, Op, srcPath, img)` | `COPY --chmod=<mode> [--chown=<uid>:<gid>] <srcPath> <path>` — `--chmod=` first, and **no `--from=`** (the source is a build-context path) where `srcPath` is the staged inline-content file |
| `EmitLinkBatch(b, []Op, img)` | One `RUN ln -sf t1 l1 && ln -sf t2 l2 …` |
| `EmitDownload(b, Op, img)` | `RUN --mount=type=cache,dst=/var/cache/charly/downloads [+ task cache:] <shell-probe> '… curl -o "$__c.part" && mv → /var/cache/charly/downloads/<sha256>; <extractor> "$__c"'` — content-addressed, fetch-once, integrity-safe |
| `EmitSetcapBatch(b, []Op, img)` | `RUN setcap -r … && setcap caps path …` |
| `EmitCmd(b, Op, layerStage, img, userIsRoot)` | `RUN --mount=type=bind,from=<layerStage>,source=/,target=/ctx [--mount=type=cache,…] sh -c 'SH=/bin/sh; [ -x /bin/bash ] && SH=/bin/bash; exec "$SH"' sh <<'OVCMD'` followed by the BUILD_ARCH/ARCH exports, `set -e`, the command, and a closing `OVCMD` (single-quoted heredoc — see below) |

### Shell-quoting and shell-prefix helpers (`spec` + `sdk/deploykit`)

Three helpers — none of them in `charly/tasks.go` — handle the shell problems that
come up when embedding scripts and JSON into a Containerfile:

| Helper | Purpose | Used by |
|--------|---------|---------|
| `spec.ShellQuote(s)` | Standard `'...'` quoting with `'\''` escape for embedded single quotes. | `EmitDownload` (the command and `t.Env` values), `EmitCmd` (`t.Env` values), `writeJSONLabel` (LABEL JSON values containing `awk '{…}'` etc.) |
| `BuildStepShellDashC()` / `BuildStepShellHeredoc()` | The build-step shell prefix: probes for bash and falls back to POSIX `sh`, so a busybox base stays buildable. One helper per emit shape (R2). | `EmitDownload` (`-c` form), `EmitCmd` (heredoc form) |

**How a multi-line `cmd:` body survives podman's parser**: a plain
`sh -c '<body>'` with embedded real newlines gets cut off by podman's
Dockerfile parser at the first newline inside the quoted string — the
parser is line-oriented and treats everything after it as a new
instruction. `EmitCmd` therefore emits a **single-quoted heredoc**
(`<<'OVCMD'`): podman's parser consumes the heredoc as one directive,
and the single-quoted delimiter means the OUTER shell performs no
expansion, so an author's `$(cmd)` and `${VAR}` reach the inner shell
intact. `exec` preserves stdin, which is what lets the chosen shell read
the body. An earlier revision used bash ANSI-C `$'…\n…'` quoting via a
`shellAnsiQuote` helper; both are gone — that form required bash, which
a busybox base does not have.

**Why `export VAR=val;` beats `VAR=val cmd`** in `EmitDownload`: the
`VAR=val cmd` prefix form sets `VAR` in `cmd`'s environment, but bash
expands `${VAR}` in `cmd`'s arguments *before* the environment is
assembled. Result: the URL `pixi-${BUILD_ARCH}-unknown-linux-musl.tar.gz`
gets rendered as `pixi--unknown-linux-musl.tar.gz` (empty arch) →
404. Terminating with `;` forces bash to run the export statement first,
putting the value in scope for the downstream URL expansion.

### Inline-content staging

`StageInlineContent(buildDir, contextRelPrefix, candyName, content)` writes `write:` task content to `<buildDir>/_inline/<layer>/<sha256>` on disk and returns the build-context-relative path (e.g. `.build/<image>/_inline/<layer>/<sha256>`). Content-addressed filename makes writes idempotent — identical content writes no-op; changed content produces a new hash which invalidates only that COPY's cache layer. **No shell heredoc carries `write:` content** — it travels as bytes through that COPY. (`EmitCmd` does emit a single-quoted heredoc for `command:` steps; the two are different paths.)

### User resolution

`ResolveUserSpec(userField, img)` → `(directive, chownPair)`:

- `root` / `0` / empty → `("0", "")` (root is COPY's default; skip `--chown`)
- `${USER}` → `(strconv.Itoa(img.UID), fmt.Sprintf("%d:%d", img.UID, img.GID))` — **numeric** `USER <UID>` directive, avoiding `/etc/passwd` dependencies at the switch point
- `<uid>:<gid>` → pass through
- bare numeric `<uid>` → `(u, u + ":" + u)`
- literal name → `(u, u + ":" + u)` (COPY `--chown=<name>:<name>` works when the user exists in the image)

### Variable substitution

Two-tier:

- **Generate-time:** `taskSubstPath` expands `~/` to `img.Home` and substitutes `${USER}` / `${UID}` / `${GID}` / `${HOME}` literally (values known at generate time). Applied to paths, URLs, modes, `to`, `target`, etc.
- **Build-time (Docker):** `${ARCH}` comes from BuildKit's `TARGETARCH` via `ARG TARGETARCH` + `ENV ARCH=${TARGETARCH}` emitted at layer top. Layer-local `var:` become `ENV` directives too. Docker substitutes these in COPY paths, RUN commands, and ENV values.
- **Build-time (shell):** `${BUILD_ARCH}` is auto-injected as a local shell variable at the top of each `command:` / `download:` RUN (`BUILD_ARCH=$(uname -m)`). Unlike `${ARCH}`, it isn't available in non-shell fields.

`taskUnresolvedRefs(s, known)` returns `${NAME}` references that don't resolve against auto-exports ∪ layer `var:` — used by `validateCandyTasks` to error on typos.

### Adjacent-coalescing (`taskCoalescesWith`)

Only these verbs coalesce: `mkdir`, `link`, `setcap`. Coalescing requires same verb + same `User` field (literal equality before resolution). The orchestrator loop consumes consecutive matching tasks into a batch, then flushes as a single directive. Non-adjacent same-verb tasks never merge — reordering across other verbs is forbidden.

### Parent-dir auto-insertion

For `copy:` and `write:` tasks, if the destination's parent directory isn't already registered in `declaredDirs` (which tracks every `mkdir:` task PLUS all ancestor paths, matching Unix `mkdir -p` semantics), the orchestrator prepends a single `RUN mkdir -p <parent>` (as the task's `run_as:`) immediately before the COPY. This is the only implicit directive in the pipeline; authors can disable it for a specific path by declaring the parent via explicit `mkdir:`.

## Tag-section install emission

Distro-version tag sections (`debian:13:`, `ubuntu:24.04:`, etc.) are resolved via first-match-wins on the image's `distro:` priority list. Tag sections use the primary format's **full** install template — so they can carry `repos:`, `keys:`, `options:`, and `package:`, not just packages alone. The generator reads each tag section's full YAML map via `charly/layers.go:TagPkgConfig.Raw`.

