# sdk/ — signpost (not the rule-set)

You are in the **OpenCharly plugin SDK**
(`github.com/opencharly/sdk`): the go-plugin serve surface (root package
`sdk`), the plugin-author helpers (`kit/`), the InstallPlan step vocabulary
(`deploykit/`), the Containerfile render machinery (`buildkit/`), the
container-engine client (`enginekit/`), the unified-config parse
(`loaderkit/`), the agent control plane (`agentkit/`), and the shared VM types
(`vmshared/`).

The wire/IR/param TYPES, the gRPC proto contract (`proto`), the CUE schema
source they derive from, AND the standalone FABRIC PRIMITIVES — the target
dial/serve transports (`transport/`), the in-process SSH client (`sshx/`, the
sole `golang.org/x/crypto/ssh` boundary), the process-lifecycle + launch leaf
(`proc/`), and the shared test fixtures (`testkit/`) — are NOT owned here; they
live in the separate contract module **`github.com/opencharly/spec`** (#55
step1 moved the four fabric slices out of the sdk kits per the granular-spec
Rule 2). This SDK is a CONSUMER of tagged `spec` releases (importing its `spec`,
`proto`, `spec/schemaconcat`, and — where a kit needs a host primitive —
`spec/proc` packages).

**Load these skills FIRST (R0):**

- `/charly-internals:plugin` — the plugin/provider model, the two authoring
  shapes, placement, and this SDK's exported surface.
- `/charly-internals:go` — the schema→spec generation recipe (in the
  `opencharly/spec` module), the CUE single-source rules, and the drift gates.
- `/charly-internals:install-plan` — the deploy wire types (CUE-sourced in the
  `github.com/opencharly/spec` module, imported here as `spec`) consumed over
  the reverse channel.

**Authoritative rules live in the superproject's root `CLAUDE.md`.** R0–R10,
hard cutover, AI attribution, and the git workflow are defined there; this file
only signposts. Schema/format changes are made in the `opencharly/spec` module
(bump its `#SchemaVersion`, regenerate, tag) and adopted here by bumping the
`require github.com/opencharly/spec` version — follow `/charly-build:migrate`
(the superproject `candy/plugin-migrate/migrations.cue` entry) and the SDK-first
landing order in `/charly-internals:git-workflow`. History lives in this repo's
`CHANGELOG/`.
