# sdk/ — signpost (not the rule-set)

You are in the **OpenCharly plugin SDK + contract repo**
(`github.com/opencharly/sdk`): the go-plugin serve surface (root package
`sdk`), the gRPC contract (`proto/`, generated from `protocol/schema/` by
`internal/wiregen` via `task wire:gen`), the CUE schema source (`schema/`),
the generated spec types (`spec/`), the plugin-author helpers (`kit/`), the
agent control plane (`agentkit/`), the target dial/serve transports
(`targetkit/`), the shared test fixtures (`testkit/`), the shared schema
concatenation (`schemaconcat/`), and the shared VM types (`vmshared/`).

**Load these skills FIRST (R0):**

- `/charly-internals:plugin` — the plugin/provider model, the two authoring
  shapes, placement, and this SDK's exported surface.
- `/charly-internals:go` — the schema→spec generation recipe (`task cue:gen`),
  the CUE single-source rules, and the drift gates.
- `/charly-internals:install-plan` — the deploy wire types in
  `spec/deploy_wire.go` consumed over the reverse channel.

**Authoritative rules live in the superproject's root `CLAUDE.md`.** R0–R10,
hard cutover, AI attribution, and the git workflow are defined there; this file
only signposts. Schema changes here are FORMAT changes for every consumer —
follow `/charly-build:migrate` (bump `schema/version.cue` + a superproject
`candy/plugin-migrate/migrations.cue` entry) and the SDK-first landing order in
`/charly-internals:git-workflow`. History lives in this repo's `CHANGELOG/`.
