---
name: generate-source
description: |-
  Containerfile generation: `charly box generate` output, multi-stage builds, intermediate images, the .build/ directory. Use when debugging/understanding generated Containerfiles or reading charly Go source.
---

# Generate - Containerfile Generation

## Overview

`charly box generate` reads `charly.yml` and `candy/`, resolves dependency graphs, and writes Containerfiles to `.build/`. Generation is idempotent and `.build/` is disposable (gitignored). Understanding the generated output is essential for debugging build issues.

Build-mode Containerfile generation is the `WriteCandySteps` → `EmitTasks` path in `sdk/deploykit` (`deploykit.Generator`, relocated from `charly/generate.go` in #67, driven by `candy/plugin-build` over the resolved-project envelope + `HostBuild("render-seam")`; the `charly/generate.go` top-level `Generate`/`writeCandySteps` orchestrator is DELETED). the pod-overlay deploy path reaches `deploykit.Generator.EmitTasks` DIRECTLY — `charly/tasks.go` holds no shim and cannot hold one (P16 import purity forbids `charly/` importing the sdk); that path stays for the pod-overlay deploy path (the candy `plugin-deploy-pod` constructs `deploykit.OCITarget` + renders via `deploykit.NewRenderGeneratorFromProject`, the per-step fragment over the `HostBuild("step-emit","oci-emit-step")` seam). The render walks each layer's ops directly to write Containerfile text — NOT the `InstallPlan` IR. The IR + `deploykit.OCITarget.Emit` is the DEPLOY-mode path: `deploykit.OCITarget` is constructed only by the candy `plugin-deploy-pod`'s `buildOverlay` for `add_candy:` overlay-Containerfile synthesis, alongside the external out-of-process deploys consuming the same IR. (The local/vm/kubernetes/android substrates are external plugins via `pluginDeployTarget` (S3b, dispatched through `candy/plugin-fleet`'s `Invoke(OpDeployDispatch)`): `deploy:local` (candy/plugin-deploy-local) and `deploy:vm` (candy/plugin-deploy-vm) DO consume the IR — the plugin walks it via `kit.WalkPlans` over the reverse channel, the vm one over the guest `SSHExecutor` so the walk runs inside the guest — while `deploy:kubernetes` (candy/plugin-kube) does NOT, materializing its Kustomize tree PLUGIN-side (`candy/plugin-kube/materialize.go`'s `materializeKustomize`, reaching `candy/plugin-k8sgen` peer-to-peer over `verb:k8sgen`/`OpEmit`).) Build shares the compiler helpers (`ResolveCascadePackages`, `CompileShellSnippetSteps`, `deploykit.RenderLocalPkgImageInstall` — one source of truth, R2, relocated to sdk/deploykit in W3) with the IR, but not the `deploykit.OCITarget` walk. For the IR shape and step kinds, see **`/charly-internals:install-plan`**. For local-deploy supporting files (ledger, builder_run, shell_profile, reverse_ops, `sdk/deploykit/compile_service_steps.go` (service_render, relocated), `candy/plugin-fleet/deploy_ref.go` (deploy_ref, relocated)), see **`/charly-internals:local-infra`**.

## Quick Reference

| Action | Command | Description |
|--------|---------|-------------|
| Generate all | `charly box generate` | Write .build/ (Containerfiles) |
| Generate with tag | `charly box generate --tag v1.0.0` | Override CalVer tag |
| List targets | `charly box list targets` | Build targets in dependency order |
| Inspect config | `charly box inspect <image>` | Show resolved config JSON |

## Generated Output Structure

```
.build/
├── <image>/
│   ├── Containerfile              # One per image
│   ├── supervisor/*.conf          # Supervisord configs (if service layers)
│   └── traefik-routes.yml         # Traefik config (if route layers)
└── _layers/<name>                 # Symlinks to remote layers
```

## Containerfile Structure

The generated Containerfile follows this order:

1. **Multi-stage build stages** — scratch stages per layer, builder stages resolved via the builder plugins' `OpResolve` leg (pixi, npm, aur, cargo — `kit.BuilderResolve`, C10), init system config assembly (driven by the embedded `init:` vocabulary), traefik routes
2. **`FROM ${BASE_IMAGE}`** — external bases get bootstrap from the embedded `distro:` vocabulary (install cmd, cache mounts, workarounds); internal bases get `USER root`
3. **Image metadata** — consolidated `ENV` directives, `EXPOSE` ports, `ai.opencharly.*` labels
4. **COPY build artifacts** — from each builder plugin's `OpResolve` reply (`CopyArtifacts` + the once-per-builder `CopyBinary`)
5. **Per-layer install steps** — see "Task emission pipeline" below. `USER` toggles as each task's `run_as:` field requires.
6. **Final assembly** — init system config assembly, traefik routes COPY, `USER <UID>`, `RUN bootc container lint` (bootc images only)

**Config-driven generation:** Format-specific install commands, cache mounts, repo setup, and init fragments are defined in the embedded build vocabulary (`charly/charly.yml`, `//go:embed`) as Go `text/template` strings — three top-level sections: `distro:`, `builder:`, `init:`. Each distro entry contains both bootstrap config and its package format definitions. The `builder:` section retains each builder's DETECTION (`detect_file`/`detect_config`), cache mounts, the deploy-time host phase, and (pixi) the runtime-env contract + `install_command`/`manylinux_fix`/`build_script` context inputs — but the build-time multi-stage TEMPLATE moved out to the builder plugins' `kit.BuilderResolve` (C10). The embedded vocabulary is parsed by the same unified loader as any project `charly.yml`. Adding a new format (e.g., `apk` for Alpine) requires only YAML changes — zero Go code modifications.

## Reference Index

| Topic | File |
|---|---|
| Task emission pipeline — per-verb emitters, `Task` struct, emitter helpers, shell-quoting, inline-content staging, user resolution, variable substitution, adjacent-coalescing, parent-dir auto-insertion, tag-section install emission | `references/task-emission.md` |
| Multi-stage build stages (pixi/npm/AUR/OpResolve builders, scratch context), auto-intermediate grouping by `(Base, UID)`, intermediate images, container user resolution (adopt vs create) | `references/multistage-and-intermediates.md` |
| LABEL placement + cache efficiency, OCI label reference table, runtime-only features, cache mounts | `references/labels-and-caching.md` |

## Common Workflows

### Debug Why a Build Fails

```bash
charly box generate                              # Generate Containerfiles
cat .build/my-image/Containerfile        # Inspect the generated Containerfile
charly box validate                              # Check for validation errors
charly box inspect my-image                      # See resolved config
```

### Understand Layer Ordering

```bash
charly box list targets                          # Shows dependency-ordered build sequence
charly box inspect my-image --format layers      # Shows layer list for an image
```

## Cross-References

- `/charly-image:layer` — **Canonical author-facing reference** for the task verb catalog, `var:` substitution, YAML anchors, execution order. The emitter pipeline here implements what's documented there.
- `/charly-build:generate` — User-facing `charly box generate` command.
- `/charly-internals:go` — Source code map: `sdk/deploykit/tasks_emit.go` (the per-verb emitters) + `tasks_render.go` (`Generator.EmitTasks`); `charly/tasks.go` is 64 lines with one function, `sdk/deploykit/candy_steps.go:WriteCandySteps` (orchestrator, relocated in #67), `charly/layers.go:Task` struct, `candy/plugin-box/validate_rules.go:validateCandyTasks`.
- `/charly-build:validate` — User-facing validation rules (what `validateCandyTasks` enforces).
- `/charly-build:build` — Building from generated Containerfiles.
- `/charly-internals:egress` — the emitted Containerfile is egress-validated (`writeContainerfile` → `#RenderedText`, rejecting the `<no value>` template-failure marker) before it is written; the traefik-routes scratch-stage input is likewise validated (`#TraefikRoutes`).
- Source: `sdk/deploykit` (render DRIVE, relocated in #67), `sdk/deploykit` (`Generator.EmitTasks` + the per-verb emitters), `sdk/deploykit` (the generator helpers — the former `charly/generate.go` is DELETED, K-wave 2), `sdk/deploykit/intermediates.go` (moved from `charly/intermediates.go`; the former `charly/intermediates_shim.go` is DELETED, K-wave 2), `sdk/deploykit/graph.go` (`ResolveBoxOrder`/`BoxNeedsBuilder` — the former thin `charly/graph_shim.go` wrappers are DELETED, K-wave 2).

## When to Use This Skill

**MUST be invoked** before reading or modifying Go source files. Invoke this skill BEFORE launching Explore agents on charly/ code.
