# opencharly/sdk — the OpenCharly plugin SDK

The module every OpenCharly plugin imports — and the mechanism library charly
core itself consumes. The wire/IR/param TYPES, the gRPC proto contract, and the
CUE schema source it all derives from live in a SEPARATE contract module,
**`github.com/opencharly/spec`**; this SDK is a CONSUMER of that module (it does
NOT own or generate them). What this repo owns:

- **`/` (package `sdk`)** — the go-plugin serve/handshake surface (`Serve`,
  `ServeCheckVerb`, `Main`, `Handshake`, `ProtocolVersion`), the executor
  reverse-channel client (`Executor`), capability building
  (`BuildCapabilities`, `ProvidedCapability`, `StepContract`), and the streaming
  channel primitives (`RelayChannel`, `SequenceGate`, `ReplayBuffer`). It
  consumes the wire types + proto stubs from `github.com/opencharly/spec`
  (`spec`, `proto`) and the schema-concatenation contract
  (`spec/schemaconcat`).
- **`kit/`** — pure helpers for plugin authors (check-verb contract, deploy
  walk, shell/render/calver utilities, the shared process launch/lifecycle
  helpers). Imports only stdlib (+ `x/sys/unix`) + the `spec` module + `vmshared`
  + yaml.
- **`deploykit/`** — the InstallPlan step VOCABULARY: the concrete deploy/render
  steps (quadlet, volumes, VM addressing, artifact retrieval, candy adapters)
  plugins compose over the shared IR.
- **`buildkit/`** — the pure Containerfile render/compute machinery.
- **`enginekit/`** — the container-engine client mechanism (the single place
  that talks to podman/docker).
- **`loaderkit/`** — the importable form of charly's unified-config PARSE (the
  parse half of `LoadUnified`), kept kind-blind via the threaded
  kind-recognition snapshot.
- **`agentkit/`** — the agent control plane: transport-independent workflow
  invariants (`Workflow`) and the daemon-free durable record store (`Store`).
- **`targetkit/`** — transport-neutral gRPC connections to Charly targets
  (`DialProvider`/`DialProcessProvider` over exec/SSH stdio processes;
  `ServeStdio` on the target side).
- **`proclifecycle/`** — a stdlib-only leaf package for process lifecycle.
- **`sshx/`** — the in-process SSH client + tunnel helpers.
- **`testkit/`** — disposable live protocol fixtures shared by SDK tests and
  consumers (e.g. the in-process SSH process server).
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
go-plugin handshake) and the schema CalVer (`kit.LatestSchemaVersion()`, which
reads the CUE-owned `spec.SchemaVersion` const from the `github.com/opencharly/spec`
module, advertised in `Capabilities.calver`).

## Schema + wire types

The CUE schema source (the single source of truth for the `charly.yml` ingress
schema), the generated Go param + wire types, the gRPC proto contract, and the
whole `cue exp gengotypes` / proto codegen pipeline live in
**`github.com/opencharly/spec`** — NOT in this repo. This SDK requires a tagged
`spec` release and imports its `spec`, `proto`, and `spec/schemaconcat`
packages. To change the schema or a wire type, edit and regenerate in the
`opencharly/spec` module, tag it, then bump the `require github.com/opencharly/spec`
version here.

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
