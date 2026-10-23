---
name: capabilities
description: |-
  OCI label contract, Capabilities/BoxMetadata struct, CapabilityLabelMap completeness, LabelService round-trip, source-less deploy via `charly deploy from-box`, or adding an OCI label. Use before any of those.
---

# Capabilities — the OCI-label runtime contract

Every opencharly image carries a complete snapshot of "what can this image do, what does it need, what does it provide" as `ai.opencharly.*` OCI labels. This skill documents the contract, the single Go type behind it, the completeness test that keeps it honest, and the source-less deploy path it enables.

Source of truth: `sdk/deploykit/capabilities.go` (the `CapabilityLabelMap` + `CapabilitiesFromLabels` + the completeness test) + `spec/spec/label_consts.go` (the label name constants) + `spec/spec/cue_types_gen.go` (`BoxMetadata`, CUE-generated) + `spec/container/box_metadata_coneb.go` (`ExtractMetadata`/`InspectLabels`). `charly/capabilities.go` and `charly/labels.go` no longer exist — both relocated out of core (FLOOR-SLIM Unit 4 / #55 coneB); `charly box labels <ref>` is now `candy/plugin-box/box.go`'s `dispatchLabels`, which reads `ai.opencharly.*` labels via `ExtractMetadata` (whole contract sorted; `--format <key>` for one raw value with a non-zero exit when absent; `--all` for non-charly labels too) — the charly-native R7 emitted-artifact check. See also `/charly-internals:go` for the broader Go architecture and `/charly-internals:install-plan` for how this feeds the build/deploy IR.

**Boundary-law note.** The `ai.opencharly.*` label contract is a **resolved-envelope** contract, not a typed-kind one (`/charly-internals:plugin` "The kernel/plugin boundary law"): an image advertises what it provides / needs / can do as generic label DATA that any consumer reads by key — the kernel never imports a concrete kind's `spec.<Kind>` type to interpret a label. A new capability field is generic label vocabulary (Data), gated by `TestCapabilityLabelCompleteness`; it never adds a per-kind branch.

## The `BoxMetadata` type — no more `Capabilities` alias

There is no `Capabilities` name anymore. The former charly-core `type Capabilities = BoxMetadata` was ZERO-ALIASES residue per the kernel/plugin boundary law (an alias is always an R-item regardless of what it aliases) — deleted; every consumer references `spec.BoxMetadata` directly (CUE-generated, `spec/spec/cue_types_gen.go`). This doc keeps the historical name in prose below only where it clarifies intent ("the capabilities contract"); the Go type to reach for is always `spec.BoxMetadata`.

Why this matters: adding a field anywhere in `spec.BoxMetadata` automatically becomes part of the capabilities contract, which means it MUST have a `CapabilityLabelMap` entry. Forget the entry and CI blocks the PR.

## `CapabilityLabelMap` — field → OCI-label name

`sdk/deploykit/capabilities.go` names every label that participates in the contract. Entry grouping (identity / account / ports / security / networking / env / engine+init / distro+builder / hooks+vm / skills / data / dependency-graph / tests) mirrors the `BoxMetadata` field ordering so the map reads as a spec of the on-disk format.

Key entries:

| Field | Label const | What it stores |
|---|---|---|
| `Version` | `LabelVersion` (`ai.opencharly.version`) | **No longer emitted** — the schema-versioning removal cutover deleted the authored `version:`, and `sdk/deploykit/write_labels.go` drops the label; it survives on the wire type only. The local-image resolver still READS it as its primary ordering key (`spec/container/local_image_coneb.go`), so with the label absent, ordering falls through to the `:<calver>` build tag. The charly-vs-non-charly presence sentinel is **`ai.opencharly.box`**, not this label. |
| `CandyVersion` | `LabelCandyVersion` (`ai.opencharly.candy_version`) | The candy's source-derived version — its **GIT TAG**, set by the loader (`sdk/loaderkit/scan_orchestrate.go` assigns `winner.GitTag`; arbiter `sdk/loaderkit/candy_version.go`). The surviving per-candy identity now that the authored `version:` is gone. |
| `Service` | `LabelService` (`ai.opencharly.service`) | **Structured JSON array of `CapabilityService`** — not just names. 23 per-entry fields including `kind`, `events`, `auto_start`, `start_retries`, `priority`, `init`, `layer`. See "LabelService" below. |
| `Init` | `LabelInit` | Init system name (supervisord / systemd / none). |
| `InitDef` | `LabelInitDef` (`ai.opencharly.init_def`) | **`*CapabilityInitDef`** — the build-resolved runtime subset of the init: vocabulary entry: `entrypoint`, `fallback_entrypoint`, `management_tool`, `management_commands`. Makes the init system TRUE single-source: `kit.ResolveEntrypointFromMeta` (container entrypoint) and `resolveInitDefFromMeta` (in-container service-management) read this label as their ONLY source. A frozen `wellKnownInitDefs` registry used to back them for pre-`init_def`-label images; it was deleted (it was triplicated across sdk/kit, plugin-pod and plugin-deploy-pod, and frozen at supervisord + systemd, so it could never answer for a third init system such as openrc). An image without the label now errors, naming the rebuild, rather than resolving a guessed management CLI. Because the contract travels in the label, init systems declared ONLY in the embedded vocabulary now work at runtime too. |
| `ServiceNames` | `LabelInit` | Per-init active-name list; baked alongside `LabelInit` for CLI ergonomics (e.g., `charly service status`). |
| `Description` | `LabelDescription` (`ai.opencharly.description`) | **Three-section `{candy, box, deploy}` `LabelDescriptionSet`** — the self-description baked into the image. Each `LabeledDescription` carries a `Description` string plus a `Plan []Step` list (each step is an intent keyword `run:`/`check:`/`agent-run:`/`agent-check:` + an inline Op); the deterministic `check:` steps are the acceptance checks consumed by `charly check live` / `charly check box`. See `/charly-check:check`. |
| `CheckLevel` | `LabelCheckLevel` (`ai.opencharly.check_level`) | The per-box acceptance-depth rung (`none` / `build` / `noagent` (default) / `agent`) gating how deep `charly check run <bed>` drives acceptance. |
| `Shell` | `LabelShell` | Three-section `{candy, box, deploy}` JSON shell-init manifest. Each entry carries an Origin (candy name / "box"), an ID for overlay keying, an optional Generic body (intrinsic init + path_append) and a per-shell ByShell map (bash/zsh/fish/sh sub-blocks). `CollectShell` (`sdk/deploykit/shell_collect.go`) populates the Candy + Box sections at `charly box build` time; `ExtractMetadata` parses the label at deploy time. Consumed by `charly box inspect` and `charly deploy from-box`. The Deploy section is now permanently empty: the validation-correctness batch retired the deploy-scope `shell:` overlay AUTHORING FIELD itself (`#Deploy.shell`/`#DeployShellOverlay` deleted from `spec/schema/deploy.cue`) — a config still carrying it is migrated away by `charly migrate`'s `strip-deploy-shell-overlay` step — so it is no longer merely "parsed but unapplied" (as an earlier pass of this doc described a transitional state); the field cannot be authored at all anymore. See `/charly-image:layer` "Shell Init Surface". |
| `EnvProvide` / `MCPProvide` | `LabelEnvProvide` / `LabelMCPProvide` | Cross-container discovery: what env vars / MCP servers this image advertises to pod peers. |
| `EnvRequire` / `MCPRequire` | `LabelEnvRequire` / `LabelMCPRequire` | What this image *needs* from peers — validated at `charly config` time. |

## `TestCapabilityLabelCompleteness` — the guardrail

`sdk/deploykit/capabilities_test.go:TestCapabilityLabelCompleteness` runs on every `go test ./...` invocation. It uses `reflect.TypeOf(spec.BoxMetadata{})` to enumerate every exported field and fails if any field is missing from `CapabilityLabelMap`:

```go
// sdk/deploykit/capabilities.go
func checkCapabilityLabelCompleteness() error {
    rt := reflect.TypeOf(spec.BoxMetadata{})
    var missing []string
    for i := 0; i < rt.NumField(); i++ {
        name := rt.Field(i).Name
        if _, ok := CapabilityLabelMap[name]; !ok {
            missing = append(missing, name)
        }
    }
    // ...
}
```

This is the enforcement mechanism that keeps the OCI-label contract and the Go struct in sync. **Workflow for adding a capability:**

1. Add the field to `BoxMetadata` in `spec/schema/boxmetadata.cue` (CUE is the single source of truth; `charly task cue-gen` regenerates `spec/spec/cue_types_gen.go`).
2. Add the label const (`LabelFoo = "ai.opencharly.foo"`) in `spec/spec/label_consts.go`.
3. Add the `CapabilityLabelMap` entry: `"Foo": LabelFoo`.
4. Emit + parse the label in `WriteLabels` (deploykit, via `writeJSONLabel`) / `ExtractMetadata`.
5. `go test ./...` passes.

Skip step 3 and the test fails with `BoxMetadata fields without CapabilityLabelMap entry: [Foo]`.

## `LabelService` — structured per-entry service data

The services label is a full structured round-trip — not a flat list of names:

```go
// spec/schema/boxmetadata.cue → spec/spec/cue_types_gen.go (CapabilityService struct, paraphrased)
type CapabilityService struct {
    Name             string
    Scope            string            // system / user
    Enable           bool
    UsePackaged      string            // name of distro-shipped unit to reuse
    Exec             string
    Env              map[string]string
    Restart          string
    WorkingDirectory string
    User             string
    After            []string
    Before           []string
    Stdout           string
    StopTimeout      string
    Kind             string            // "program" (default) | "eventlistener"
    Events           string            // required when Kind == "eventlistener"
    AutoStart        *bool             // three-state; supervisord autostart=
    StartRetries     int
    StartSec         int
    StopSignal       string
    ExitCode         string
    Priority         int
    Init             string            // which init owns it (supervisord / systemd)
    Layer            string            // source layer name
}
```

**Why this matters**: `charly deploy from-box` (see below) reconstructs the full deploy surface from OCI labels alone. A names-only `Service` label would leave deploy-time Kubernetes manifest generation blind to whether a process needs `start_retries: 3` or is an eventlistener. The structured label carries every supervisord directive faithfully, and the Kubernetes Kustomize generator (see `/charly-kubernetes:kubernetes`) reads from it without touching the source repo.

## Source-less deploy: `charly deploy from-box`

`deploykit.CapabilitiesFromLabels(engine, imageRef)` (`sdk/deploykit/capabilities.go`) is the source-less entry point: given an engine + image ref, it runs `ExtractMetadata` (which pulls labels via `podman inspect` / `docker inspect`), returns a fully-populated `*spec.BoxMetadata`, and every downstream consumer (deploy target, Kubernetes generator, quadlet generator) reads from that struct. `candy/plugin-fleet/deploy_from_box.go` is `charly deploy from-box`'s own caller.

```go
caps, err := deploykit.CapabilitiesFromLabels("podman", "ghcr.io/opencharly/fedora-coder:latest")
// caps.Service[0].Kind == "eventlistener" works — no source repo needed
```

This is what enables the "**Kubernetes deploy without access to charly.yml**" invariant: a Kustomize overlay can be generated from a published image alone, for dev/staging/prod clusters that never see the build repo.

## Three-layer image architecture (planned schema split)

The long-term direction (aspirational — not yet started in code or schema) is to split `BoxConfig` into three discriminated sections:

- `box.build:` — Containerfile inputs (base image, layers, distro/builder selection). Consumed only by `charly box build`.
- `box.capabilities:` — the runtime contract documented here. Emitted as OCI labels.
- `box.deploy:` — target-specific defaults (Kubernetes storage class, container-target port defaults). Consumed by `charly deploy add`.

Today these co-exist in a single `BoxConfig`. The `CapabilityLabelMap` completeness test will keep the contract honest across the migration (once the schema split lands, `spec.BoxMetadata` will project the `capabilities:` subsection directly instead of the whole struct).

See `/charly-image:image` for current user-facing structure and `/charly-build:migrate` for the one-shot `charly migrate` converter that emits the new schema when the split lands.

## Adding a new OCI label — checklist

1. Add the const to `spec/spec/label_consts.go` (`LabelFoo = "ai.opencharly.foo"`).
2. Add the field to `BoxMetadata` (`spec/schema/boxmetadata.cue`, CUE-generated) with a matching JSON tag.
3. Add the `CapabilityLabelMap` entry: `"Foo": LabelFoo`.
4. Populate it in `WriteLabels` (deploykit label emission at build time, via `writeJSONLabel` for struct/list values).
5. Parse it in `ExtractMetadata` (label read-back at deploy time).
6. If the field is a struct/list, route it through `writeJSONLabel` for consistent encoding (see how `LabelService`, `LabelDescription`, `LabelVolume` do it).
7. `go test ./...` — `TestCapabilityLabelCompleteness` passes.
8. Update `/charly-internals:capabilities` (this skill) with the new entry in the key-labels table.

## See Also

- `/charly-image:image` — user-facing `candy:` image entries (with `base:`/`from:`) in `charly.yml`
- `/charly-image:layer` — user-facing `candy:` authoring, including `service:` which feeds `LabelService`
- `/charly-core:deploy` — `charly deploy add` / `charly deploy from-box` / `charly deploy status` commands
- `/charly-kubernetes:kubernetes` — Kubernetes deploy target that reads `LabelService` to generate Kustomize
- `/charly-check:check` — three-section `LabelDescription` (candy/box/deploy) — same label-contract pattern
- `/charly-build:migrate` — `charly migrate` — emits the schema that populates these labels
- `/charly-internals:go` — Go architecture overview, `LoadUnified`, `parseCandyYAML`
- `/charly-internals:install-plan` — internal IR shared across build and deploy pipelines
