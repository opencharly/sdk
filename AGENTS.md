# AGENTS.md — opencharly/sdk

The OpenCharly plugin SDK (`github.com/opencharly/sdk`): the go-plugin serve
surface (root package `sdk`), the plugin-author helpers (`kit/`), the InstallPlan
step vocabulary (`deploykit/`), the Containerfile render machinery (`buildkit/`),
the container-engine client (`enginekit/`), the unified-config parse
(`loaderkit/`), the agent control plane (`agentkit/`), and the shared VM types
(`vmshared/`). It is a CONSUMER of the separate contract module
**`github.com/opencharly/spec`** (wire/IR/param types, proto, CUE schema source,
fabric primitives) — it does not own or generate them.

Canonical files:

- `go.mod` — the module (`github.com/opencharly/sdk`) and its pinned
  `require github.com/opencharly/spec`.
- `sdk.go`, `verb.go`, `executor.go`, `channel.go`, `schema.go` — the root-package
  serve/handshake, executor reverse-channel, capability and streaming primitives.
- `kit/`, `deploykit/`, `buildkit/`, `enginekit/`, `loaderkit/`, `agentkit/`,
  `vmshared/` — the per-surface mechanism packages.
- `.github/workflows/ci.yml` — the Go gates (gofmt, vet, test, cross-compile
  matrix for `amd64`, `arm64`, `arm`, `386`).
- `README.md` — user overview only; never agent guidance.

## Load these skills first (R0)

- `/charly-internals:plugin` — the plugin/provider model, the two authoring
  shapes, placement, and this SDK's exported surface.
- `/charly-internals:go` — the schema→spec generation recipe (in the
  `opencharly/spec` module), the CUE single-source rules, and the drift gates.
- `/charly-internals:install-plan` — the deploy wire types (CUE-sourced in
  `spec`, imported here as `spec`) consumed over the reverse channel.

## Build / validate / test

- `go build ./...` and `go test ./...` — the module's Go gates.
- CI (`.github/workflows/ci.yml`) also runs `test -z "$(gofmt -l .)"`,
  `go vet ./...`, and a cross-compile matrix (`GOOS=linux`, `GOARCH` in
  `amd64`/`arm64`/`arm`/`386`, `CGO_ENABLED=0`).
- The merge gate is the **org-wide** `charly/pr-validator` (required check
  `validate / validate`, defined in `opencharly/.github`); this repo carries no
  per-repo candy gate.

## Modify this repo

- Schema and wire types are NOT here: edit them in `github.com/opencharly/spec`
  (bump its `#SchemaVersion`, regenerate, tag), then bump the
  `require github.com/opencharly/spec` version here and adopt. Follow
  `/charly-internals:go` and the SDK-first landing order in
  `/charly-internals:git-workflow`.
- This is a reusable MECHANISM library consumed by more than one plugin: keep it
  kind-blind and free of capability code (the placement test in
  `/charly-internals:plugin`).
- Go-module tags follow the sdk scheme
  `v0.<YYYYDDD>.<HHMM with leading zeros stripped>` — NOT the superproject's
  `v<YYYY.DDD.HHMM>` (a semver requirement, not a choice).

## Landing

- PR-only. Every change lands through a pull request; the org-required
  `charly/pr-validator` validates the diff and body and arms native auto-merge on
  PASS. Direct pushes to `main` are blocked.
- History lives in `CHANGELOG/` (written by `tag-on-merge` at merge time); the PR
  body IS the changelog.
- The authoritative rulebook is the umbrella `AGENTS.md` in
  `opencharly/opencharly` and `charly/AGENTS.md` in the charly repo. Do not
  restate its rules here.
