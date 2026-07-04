# opencharly/sdk — the OpenCharly plugin SDK + contract repo

The single module every OpenCharly plugin imports — and the CONTRACT charly
core itself consumes. It owns:

- **`/` (package `sdk`)** — the go-plugin serve/handshake surface (`Serve`,
  `ServeCheckVerb`, `Main`, `Handshake`, `ProtocolVersion`), the executor
  reverse-channel client (`Executor`), capability building
  (`BuildCapabilities`, `ProvidedCapability`, `StepContract`), and shared
  verb/deploy helpers.
- **`proto/`** — `plugin.proto` + generated gRPC stubs: the four services
  (`PluginMeta`, `Provider`, `ExecutorService`, `CheckContextService`).
- **`spec/`** — the GENERATED Go param + wire types (`cue exp gengotypes` over
  `schema/`), plus the hand-written union/alias/method files. Never hand-edit
  the `*_gen.go` files; run `task cue:gen`.
- **`schema/`** — the CUE schema source (single source of truth for the
  `charly.yml` ingress schema) exported as an embedded FS (`schema.FS`).
- **`kit/`** — pure helpers for plugin authors (check-verb contract, deploy
  walk, shell/render/calver utilities). Imports only stdlib + `spec` + yaml.
- **`schemaconcat/`** — the ONE schema-concatenation contract shared by the
  runtime validator, the loader's base++plugin splice, and dev-time codegen.
- **`vmshared/`** — VM rendering + orchestration types shared by charly core
  and the VM-facing plugins (libvirt YAML/XML, qemu argv, cloud-init, OVMF,
  SMBIOS, ssh client/tunnel helpers).

## Versioning

Go module tags follow **`v0.<YYYYDDD>.<HHMM with leading zeros stripped>`**,
derived from the same UTC CalVer the superproject uses for its
`v<YYYY.DDD.HHMM>` release tags (which are NOT valid Go module versions).
Mapping example: superproject `v2026.185.0751` ⇄ sdk `v0.2026185.751`. Tags are
immutable and add-only; minor (`YYYYDDD`) and patch (minutes-of-day) sort
chronologically under semver comparison.

The plugin PROTOCOL gates are carried separately: `sdk.ProtocolVersion` (the
go-plugin handshake) and the schema CalVer (`kit.LatestSchemaVersion()`,
generated from `schema/version.cue`, advertised in `Capabilities.calver`).

## Regeneration

- `task cue:gen` — regenerates `spec/{cue_types_gen,vocab_gen,version_gen}.go`
  from `schema/*.cue` (self-bootstraps the pinned cue CLI into `./bin/cue`).
  Reproducibility-gated by `TestGenReproducible`.
- `task proto:gen` — regenerates `proto/*.pb.go` from `proto/plugin.proto`
  (self-bootstraps the pinned protoc + Go codegen plugins into `./bin`).

## Consumers

- **charly core** (`github.com/opencharly/charly/charly`) requires this module
  and mounts this repo as the `sdk/` git submodule (in-tree builds resolve via
  `replace github.com/opencharly/sdk => ../sdk`).
- **Every plugin candy** (`candy/plugin-*` in the charly monorepo, and any
  out-of-tree plugin) requires ONLY this module — never charly core. An
  out-of-tree plugin requires a tagged version; in-monorepo candies use
  `replace github.com/opencharly/sdk => ../../sdk`.

Authoring reference: the `/charly-internals:plugin` skill in the
`opencharly/plugins` marketplace.
