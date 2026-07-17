// CUE schema for the COMMAND-time host↔plugin seam wire types (P10). A
// compiled-in command plugin (candy/plugin-vm's command:vm leg) owns its CLI
// handlers but cannot LoadUnified, hold the deploy ledger, or run the VM-disk
// build engine — those are core Mechanisms a plugin (a separate module importing
// only sdk) reaches over the in-proc reverse channel: config → HostBuild(
// "config-resolve"), ledger writes → HostBuild("config-persist"), VM-disk build →
// HostBuild("vm-build"). Each action noun is CLASS-GENERIC (never a substrate
// word — the F11 uniform-API gate); pod (P11) + bundle (P13) reuse the same seams.
//
// Package-less; concatenated into the spec compilation unit. NOT authoring kinds
// (never in #Node/#Op) — pure generated wire types, single-sourced here so
// `task cue:gen` produces the Go structs (WIRE TYPES ARE CUE-SOURCED WITHOUT
// EXCEPTION, CLAUDE.md SDD). Fields that carry a hand-written runtime type with
// NO CUE def (*ResolvedVm, map[string]*ResolvedResource) travel as opaque
// bytes/RawBody envelopes the consumer marshals/unmarshals at the boundary (the
// #ParsedNode `body: bytes @go(...,type=RawBody)` idiom); fields whose type HAS a
// CUE def reference it directly (#Deploy is fully generated; #VmDeployState is a
// def with a hand materialization, referenced via @go(...,type=*VmDeployState) —
// the same shape #Deploy.vm_state uses). @go names match the Go field names the
// host + plugin consumers reference so the CUE-source flip is call-site-invisible.

// #ConfigResolveRequest asks the host to resolve the project config for one
// entity. Entity is the resolved entity name (a kind:vm entity for command:vm;
// empty resolves the project-wide enumeration in VmEntities). Dir is the project
// dir (empty → the host uses its own cwd), matching the LoadUnified(dir) contract.
#ConfigResolveRequest: {
	entity!: string @go(Entity)
	dir?:    string @go(Dir)
}

// #ConfigResolveReply is the host-resolved config data. For a kind:vm entity:
// VmJSON is the resolved vm value envelope (uf.VM[entity] via resolveVmViaPlugin,
// #Vm-defaulted host-side), ResourcesJSON the resolved resource map
// (uf.resolveResources() — drives GPU auto-allocation) — both opaque JSON of a
// hand-written runtime type with no CUE def. Backend is the resolved vm backend
// (resolveVmBackend, which also starts the libvirt user session); Claimant +
// ClaimantNode carry the exclusive-resource claimant (lookupVMClaimant) the
// handler acquires a preempt lease for. VmBackend/BuildEngine/RunEngine are the
// runtime-settings fields (ResolveRuntime) the create/build pipeline reads.
// VmState is the entity's persisted deploy-ledger runtime state (instance-id,
// ssh_port, disk path) — the READ half of the ledger dep (loadDeployConfigForRead
// → LookupKey "vm:<entity>") so the plugin reuses the persisted auto-port +
// regenerates the seed ISO without holding the deploy-config lock. VmEntities is
// the project's declared kind:vm entity NAMES (the keys of uf.VM) — the
// enumeration `charly vm import` needs to detect name conflicts. Fields absent
// for an entity that does not need them stay zero.
#ConfigResolveReply: {
	vm_json?:        bytes  @go(VmJSON, type=RawBody)
	resources_json?: bytes  @go(ResourcesJSON, type=RawBody)
	backend?:        string @go(Backend)
	claimant?:       string @go(Claimant)
	claimant_node?:  #Deploy @go(ClaimantNode, optional=nillable)
	vm_backend?:     string @go(VmBackend)
	build_engine?:   string @go(BuildEngine)
	run_engine?:     string @go(RunEngine)
	vm_state?:       #VmDeployState @go(VmState, type=*VmDeployState)
	vm_entities?: [...string] @go(VmEntities)
}

// #PodDisposableRequest asks the host whether a per-host POD deploy overlay entry is disposable
// (K5-U2/3). This is the ONE AI-harness check-project fact the resolved-project envelope cannot
// carry: the harness's iterate sandbox is an OPERATOR-provisioned per-host deploy (`charly bundle
// add <sandbox> <ref> --disposable`), so its disposability lives in the per-host overlay
// (LoadBundleConfig → ~/.config/charly/charly.yml), NOT the project charly.yml the resolved-project
// envelope projects (Mode Purity keeps the overlay out of the build-mode projection). The overlay
// read needs the core loader a plugin cannot import, and no deploy/status provider serves it, so it
// rides this THIN retained host seam. The host returns Bundle[Name].IsDisposable() (false when the
// sandbox has no entry — the harness then skips its fresh-per-run restart). Class-generic action
// noun "pod-disposable" (F11 — never a substrate word).
#PodDisposableRequest: {
	name!: string @go(Name) // the per-host pod deploy name (the iterate sandbox)
}

// #PodDisposableReply carries the single overlay-disposability bit.
#PodDisposableReply: {
	disposable?: bool @go(Disposable)
}

// #ConfigPersistRequest is the WRITE twin of config-resolve: a command plugin
// asks the host to persist (or remove) an entity's deploy-ledger entry. The host
// owns the ledger + its blocking acquireDeployConfigLock (a core Mechanism — the
// plugin is a separate module and MUST NOT hold a process-shared file lock across
// the boundary), so the plugin resolves its intent into this envelope and the
// host applies it under the lock. Key is the full deploy key ("vm:<name>"); Remove
// deletes the entry (destroy), else Entity + VmState are saved (create
// persist-auto-port). The action noun "config-persist" is class-generic (F11).
#ConfigPersistRequest: {
	key!:      string @go(Key)
	entity?:   string @go(Entity)
	vm_state?: #VmDeployState @go(VmState, type=*VmDeployState)
	remove?:   bool @go(Remove)
}

// #ConfigPersistReply is the config-persist host-builder reply — empty; the host
// signals failure via its error return (surfaced to the plugin over the channel).
#ConfigPersistReply: {}

// #VmBuildRequest carries the `charly vm build` command flags (the former
// VmBuildCmd fields). The host resolves the kind:vm entity + the build vocabulary
// + the per-source-kind image refs into a #VmBuildReply envelope (the "vm-build"
// host-builder is PREP+RESOLVE only, P8b-rest); the plugin's `charly vm build`
// command runs the actual privileged-container / qemu-img / bootc-install / cloud-init
// disk-build ENGINE itself, exactly as candy/plugin-build's podman DRIVE runs behind
// HostBuild("build-prep") (P8b) — the same inversion, applied to the VM disk-build engine.
// force skips the cloud_image content-freshness check, forcing a base-disk rebuild even when
// unchanged (P8b-rest: `--force` predates command:vm's P10 externalization but was dropped from
// this seam then — restored here since BuildCloudImage's force parameter is load-bearing).
#VmBuildRequest: {
	box!:       string @go(Box)
	size?:      string @go(Size)
	root_size?: string @go(RootSize)
	tag?:       string @go(Tag)
	type?:      string @go(Type)
	transport?: string @go(Transport)
	console?:   bool   @go(Console)
	force?:     bool   @go(Force)
}

// #VmBuildReply is the "vm-build" host-builder reply (P8b-rest): everything the
// plugin needs to run the disk-build engine without importing the loader. VmJSON is
// the resolved+validated kind:vm entity (the #Vm-shaped value resolveVmViaPlugin
// already produces — opaque bytes, the SAME convention #ConfigResolveReply.vm_json
// uses for a #Vm-shaped payload) so the plugin decodes it into its own spec.Vm rather
// than re-parsing uf.VM[entity] itself (which needs LoadUnified, a core Mechanism).
// DistroJSON/BuilderJSON carry the matched *DistroDef/*BuilderDef (bootstrap source
// only) — hand-written runtime types with no CUE def, so they ride as opaque RawBody
// too (the established idiom this file documents at the top). Engine/Rootful are the
// resolved runtime settings (ResolveRuntime) the engine needs to pick `podman` vs
// `sudo podman`. BootcImageRef/BuilderImageRef are PRE-RESOLVED (and, for the builder
// image, pre-built via `charly box build`) — both need the local podman-storage +
// project-config lookup a plugin cannot do (resolveBootcImageRef / ensureBuilderImageBuilt
// stay host-side). OutputDir/VmStateDir are the resolved per-entity paths (vmshared.VmDiskDir
// is ALREADY plugin-importable, but the host still resolves+creates VmStateDir since it also
// reads the existing ledger state below). ExistingState is the entity's persisted
// VmDeployState (#VmDeployState already has a CUE def — a typed embed, not opaque) so the
// plugin reuses the same instance-id / regenerates the seed ISO idempotently.
#VmBuildReply: {
	source_kind!:       string          @go(SourceKind) // cloud_image | bootc | bootstrap
	vm_json!:           bytes           @go(VmJSON, type=RawBody)
	distro_json?:       bytes           @go(DistroJSON, type=RawBody)
	builder_json?:      bytes           @go(BuilderJSON, type=RawBody)
	engine?:            string          @go(Engine)
	rootful?:           bool            @go(Rootful)
	bootc_image_ref?:   string          @go(BootcImageRef)
	builder_image_ref?: string          @go(BuilderImageRef)
	output_dir!:        string          @go(OutputDir)
	vm_state_dir!:      string          @go(VmStateDir)
	existing_state?:    #VmDeployState  @go(ExistingState, type=*VmDeployState)
	force?:             bool            @go(Force)
}

// #DeployAddRequest carries the `charly bundle add` command flags (the former
// BundleAddCmd's authored fields). The command:bundle plugin (P13) owns the CLI
// GRAMMAR but cannot drive the deploy KERNEL — the loader, the InstallPlan
// compiler, ResolveTarget → externalDeployTarget, and the live-executor
// composition (which threads host objects that cannot cross the process boundary)
// are core Mechanisms. So the plugin's `charly bundle add` command is THIN — it
// forwards these flags to HostBuild("deploy-add"), and the host runs the existing
// add orchestration VERBATIM (Run → dispatchNode → compile → ResolveTarget → Add),
// exactly as the box-build engine stayed core behind HostBuild("image") in P8 and
// the VM-disk engine behind HostBuild("vm-build") in P10. The two per-node internal
// fields (vmEntity, builderImageOverride) are NOT carried — the host derives them
// during dispatch.
#DeployAddRequest: {
	name!:               string @go(Name)
	ref?:                string @go(Ref)
	add_candy?: [...string] @go(AddCandy)
	tag?:                string @go(Tag)
	dry_run?:            bool   @go(DryRun)
	node_only?:          bool   @go(NodeOnly)
	format?:             string @go(Format)
	pull?:               bool   @go(Pull)
	verify?:             bool   @go(Verify)
	with_services?:      bool   @go(WithServices)
	allow_repo_changes?: bool   @go(AllowRepoChanges)
	allow_root_tasks?:   bool   @go(AllowRootTasks)
	skip_incompatible?:  bool   @go(SkipIncompatible)
	builder_image?:      string @go(BuilderImage)
	assume_yes?:         bool   @go(AssumeYes)
	disposable?:         bool   @go(Disposable)
	lifecycle?:          string @go(Lifecycle)
}

// #DeployAddReply is the "deploy-add" host-builder reply — empty; the add prints its
// own progress + dry-run output to the shared stdio (the compiled-in plugin's
// HostBuild runs in charly's own process) and signals failure via the error return.
#DeployAddReply: {}

// #DeployDelRequest carries the `charly bundle del` command flags. The plugin's
// `charly bundle del` forwards these to HostBuild("deploy-del"); the host runs the
// existing del orchestration VERBATIM (resolveDelNode → ResolveTarget → Del,
// replaying the recorded ReverseOps). The live ReverseRunner is NOT carried — a
// programmatic teardown that needs a specific runner (the vm guest-SSH reverse
// runner) is a host-side path, resolved during dispatch, never authored on the CLI.
#DeployDelRequest: {
	name!:              string @go(Name)
	assume_yes?:        bool   @go(AssumeYes)
	keep_repo_changes?: bool   @go(KeepRepoChanges)
	keep_services?:     bool   @go(KeepServices)
	keep_image?:        bool   @go(KeepImage)
	dry_run?:           bool   @go(DryRun)
}

// #DeployDelReply is the "deploy-del" host-builder reply — empty (prints host-side,
// errors via the return).
#DeployDelReply: {}

// #DeployFromBoxRequest carries the `charly bundle from-box` command flags (the
// former BundleFromBoxCmd) — a SOURCE-LESS deploy driven entirely by an image's
// baked OCI labels. The plugin forwards these to HostBuild("deploy-from-box"); the
// host runs the existing from-box orchestration VERBATIM (the project-free runConfig
// core via BoxConfigSetupCmd, or the K8s Kustomize path with --cluster).
#DeployFromBoxRequest: {
	ref!:       string @go(Ref)
	name?:      string @go(Name)
	instance?:  string @go(Instance)
	env?: [...string] @go(Env)
	port?: [...string] @go(Port)
	cluster?:   string @go(Cluster)
	namespace?: string @go(Namespace)
}

// #DeployFromBoxReply is the "deploy-from-box" host-builder reply — empty (prints
// host-side, errors via the return).
#DeployFromBoxReply: {}

// #DeployConfigRequest carries a `charly bundle` CONFIG-MANAGEMENT subcommand
// (show/export/import/reset/status) — the per-host deploy-overlay read/write ops
// that consult LoadUnified (a core Mechanism the plugin cannot import). Op selects
// the subcommand; the remaining fields carry that subcommand's authored inputs. The
// plugin forwards these to HostBuild("deploy-config"); the host runs the existing
// handler VERBATIM, printing to the shared stdio. (`path` is NOT here — it resolves
// via kit.DefaultDeployConfigPath entirely plugin-side, no seam.)
#DeployConfigRequest: {
	op!:        string @go(Op) // show | export | import | reset | status
	box?:       string @go(Box)
	instance?:  string @go(Instance)
	boxes?: [...string] @go(Boxes)
	output?:    string @go(Output)
	all?:       bool   @go(All)
	files?: [...string] @go(Files)
	replace?:   bool   @go(Replace)
}

// #DeployConfigReply is the "deploy-config" host-builder reply — empty (the handler
// prints its output to the shared stdio, errors via the return).
#DeployConfigReply: {}

// #PodConfigWriteRequest carries the POD config-WRITE (P11). Under Ruling C the config-WRITE
// (the quadlet/.pod/sidecar/tunnel file generation) moved to the deploy:pod plugin, while the
// RESOLVE + host side-effects (secret provisioning, saveDeployState, enc-mount, data-seed,
// systemctl) stay in the HOST `charly config` command (Q1=(a)). So this is HOST→PLUGIN: for a
// pod deploy, `charly config` resolves the full QuadletConfig + computes the exact target
// PATHS (the core filename helpers, unchanged) and PUSHES them to the plugin's config-write Op,
// which generates the file contents (deploykit.GenerateQuadlet + the pod/sidecar/tunnel
// generators) and os.WriteFiles them — byte-identical to the former core write phase (same
// paths, same content, same modes: .container/.pod/sidecar 0600, tunnel .service 0644).
//
// PodConfigJSON is the resolved deploykit.QuadletConfig — a hand-written runtime type with no
// CUE def, so it travels as an opaque RawBody envelope (the VmJSON pattern; no new CUE wire
// struct). An optional path field being SET is the host's signal to write that file kind
// (pod_path/sidecar_paths present ⇒ sidecars configured; tunnel_path present ⇒ cloudflare
// tunnel) — the host owns the write conditionals, the plugin writes what it is told.
#PodConfigWriteRequest: {
	pod_config_json!:      bytes             @go(PodConfigJSON, type=RawBody) // resolved deploykit.QuadletConfig
	container_path!:       string            @go(ContainerPath)              // full path for the .container quadlet
	pod_path?:             string            @go(PodPath)                    // full path for the .pod (set iff sidecars present)
	sidecar_paths?: {[string]: string}       @go(SidecarPaths)               // sidecar name → full .container path
	tunnel_path?:          string            @go(TunnelPath)                 // full path for the cloudflare tunnel .service
	cloudflared_cfg_path?: string            @go(CloudflaredCfgPath)         // cloudflared config path for GenerateTunnelUnit's ExecStart
}

// #PodConfigWriteReply returns the paths the plugin actually wrote (deterministic; the host
// already knows them — used for the byte-parity assertion + teardown provenance).
#PodConfigWriteReply: {
	written_paths?: [...string] @go(WrittenPaths)
}

// #PodLifecyclePlan is the host-resolved pod-lifecycle carrier (the K4 deep-body move): the pod
// start/stop/shell RESOLUTION stays host-side (config_image/deploy/network/enc/tunnel = #59
// inventory) and FILLS this plan, which the host threads on the F6 OpStart/OpStop/OpShell
// op.Params; candy/plugin-deploy-pod EXECUTES it — running the container start/stop over the served
// host executor and composing enc + tunnel via InvokeProvider(verb:enc/verb:tunnel), so the former
// podCli("start"/"stop"/…) `charly`-reentries are DELETED (bodies, not shells). The pre-built enc
// verb input (spec.EncExecInput — a hand-written wire type with no CUE def) rides as an opaque
// RawBody envelope (empty ⇒ that leg is skipped, the common plain-pod case) with its Method set
// per-op host-side; tunnel references the CUE-def'd #TunnelConfig directly and the plugin infers
// start-vs-stop from the op. The ARBITER claim is NOT threaded here — its CHARLY_PREEMPT_LEASE
// machinery is host-PROCESS state a placement-agnostic plugin cannot own, so the host proxy BRACKETS
// the plugin op (acquire before OpStart; release after OpStop + on the failure path).
// #PodExecReply is the reply from the pod plugin's OpShell CAPTURED-exec leg (the K4 `charly service`
// move — an in-container init-mgmt exec, non-interactive). The plugin RunCaptures the argv over the
// served executor and returns the combined Output + the exact ExitCode; the host reprints Output
// (placement-agnostic: an out-of-process plugin's stdout is NOT charly's) and propagates a non-zero
// ExitCode as *sdk.ExitCodeError so `charly service` preserves the container command's exit code
// exactly (the passthrough→capture semantics change the ruling requires be exit-code-faithful).
#PodExecReply: {
	output?:    string @go(Output)
	exit_code?: int    @go(ExitCode,type=int)
}

#PodLifecyclePlan: {
	mode!:           "quadlet" | "direct" @go(Mode)           // runQuadlet (systemctl) vs runDirect (podman run)
	svc_name?:       string               @go(SvcName)        // serviceNameInstance — quadlet unit
	container_name!: string               @go(ContainerName)  // containerNameInstance — engine target
	run_argv?: [...string] @go(RunArgv)                        // buildStartArgs output — direct mode `podman run -d`
	direct_deploy?:  bool                 @go(DirectDeploy)    // IsDirectDeploy — quadlet-absent `podman start` fallback
	engine_bin!:     string               @go(EngineBin)      // EngineBinary(resolved engine)
	unmount?:        bool                 @go(Unmount)        // `charly stop --unmount` — enc FUSE teardown
	enc?:     bytes @go(Enc, type=RawBody) // pre-built spec.EncExecInput (Method ensure@start / unmount@stop)
	tunnel?:  #TunnelConfig @go(Tunnel,optional=nillable) // resolved tunnel config (nil ⇒ no tunnel) — TunnelStart@start / TunnelStop@stop
}

// #PodLiveStdioPlan is the F12 host-resolved LIVE-STDIO carrier — ONE carrier for shell + cmd + logs
// (identical shape, R3; the OP + the executor method distinguish them). Like #PodLifecyclePlan the
// RESOLUTION stays host-side (#59 inventory fills `script` with the exact venue command); the host
// threads it on the F6 OpAttach (shell/cmd) / OpLogs (logs --follow) op.Params, and
// candy/plugin-deploy-pod EXECUTES it over the served host executor via exec.RunInteractive
// (OpAttach — inherited LIVE stdin/stdout/stderr; the child `podman exec -it`/`-i` owns the PTY +
// resize + Ctrl-C) / exec.RunStream (OpLogs — inherited LIVE stdout/stderr, no stdin). UNARY: the
// host reverse-server runs IN the charly process, so os.Stdin/os.Stdout = the operator's terminal —
// stdio NEVER crosses the wire, only `script` + the session exit code (the hostBuildCli doctrine).
// This takes the F12 exit for the shell/cmd/logs-follow rows of the #57 M-core register: the former
// inline `charly shell`/`cmd` bodies + the podCli("logs") reentry are DELETED (bodies, not shells).
#PodLiveStdioPlan: {
	// resolved venue command:
	//   shell → `podman exec -it charly-<box> bash [-c cmd]` OR the ephemeral `podman run --rm -it … bash`
	//   cmd   → `<engine> exec [-e env] charly-<box>[-<sidecar>] sh -c <command>` (no -t; stdin piped)
	//   logs  → `<engine> logs [-f] [-n N] charly-<box>` OR quadlet `journalctl --user -u <svc> [-f] [-n N]`
	script!: string @go(Script)
}

// #CheckRunRequest asks the host to RUN a check plan against a venue and return the
// per-step results (P12). command:check (candy/plugin-check) owns the `charly check`
// CLI + output formatting, but RUNNING a plan is a composite of core host-serving
// Mechanisms a plugin (a separate module importing only sdk) cannot perform: the
// venue→executor construction, the OCI-label plan extraction, and the plan-walk's verb
// dispatch through the provider registry. So the plugin resolves its intent into this
// envelope and the host builds the venue + runs the kit-Runner through the in-core
// registry VerbResolver, exactly as command:vm forwards `vm build` to
// HostBuild("vm-build"). The action noun "check-run" is class-generic (F11).
//
// Mode selects the run shape (discriminated union): "box" — a pure-box run against a
// disposable container built from Image (RunModeBox, build-scope steps only, the CheckBoxCmd
// engine); "live" — a full-stack run against a running deployment resolved by Name (the host
// classifies vm/pod/local/group internally, so the plugin stays kind-blind), applying the
// Instance/Section/Filter selectors; "feature-box" / "feature-live" — the ADE acceptance run
// (SkipDeterministicRun) over Image (build scope) or the live deployment Name (deploy scope,
// the host-side agent grader wiring, gated by NoAgent/Agent/Timeout), scoped by Tag/Strict.
// Dir is the project dir (empty → the host uses its own cwd), matching LoadUnified(dir).
// `format` is deliberately NOT a field — the plugin formats the returned Steps itself.
// run-bed + iterate are NOT seam modes: the plugin drives them over HostBuild("cli").
//
// The REPLY is NOT a CUE wire type: it is kit.CheckRunReply (sdk/kit/checkrun_seam.go),
// which carries []kit.StepResult verbatim so the plugin reuses the kit formatters
// (FormatStepResults*) with byte-parity across every --format (json marshals the full
// StepResult incl. *spec.Op). A live `cue exp gengotypes` spike proved kit.CheckResult
// is genuinely inexpressible in CUE — its engine-internal `DeadlineExceeded bool
// json:"-"` field has no gengotypes construct — so the reply rides with the engine's
// hand-written result model in kit (the wire-mandate's spike-proven exception path).
//
// P12 Wave-2: the "score" mode adds Plan — a substituted, nonce-carrying scoring plan the
// host walks via RunCheckLive (NOT the OCI-baked plan the "live" mode extracts). Its per-step
// scoring verdicts ride the kit.CheckRunReply.Score field (a *CheckRunResults, below).
#CheckRunRequest: {
	mode!:     string @go(Mode)
	name?:     string @go(Name)
	image?:    string @go(Image)
	instance?: string @go(Instance)
	dir?:      string @go(Dir)
	section?:  string @go(Section)
	filter?: [...string] @go(Filter)
	tag?:      string @go(Tag)
	strict?:   bool @go(Strict)
	agent?:    string @go(Agent)
	timeout?:  string @go(Timeout)
	no_agent?: bool @go(NoAgent)
	plan?: [...#Step] @go(Plan) // "score" mode: the substituted scoring plan RunCheckLive walks
}

// #CheckRunResults / #StepScore / #ScoreSummary — the AI-harness SCORING result model (P12
// Wave-2). RunCheckLive returns a *CheckRunResults (the scored check:/agent-check: verdicts,
// keyed by step id for plateau tracking); it doubles as the `charly check box --format yaml`
// payload the harness scorer parses (ParseCharlyTestOutput). These are plain structs — the
// gengotypes workhorse — CUE-sourced so BOTH core (RunCheckLive, the "score"-mode reply's
// Score field) and the relocated plugin scorer import ONE definition (SDD; no alias). Every
// field mirrors the former hand-written Go tag set: required (!) fields carry no json-omitempty
// (json wire byte-identical for the seam reply); optional (?) fields carry it. The retag pass
// adds ,omitempty to every YAML tag uniformly — inert here since ID/Status are always set and a
// zero Summary block only elides on an empty (0-step) result ParseCharlyTestOutput tolerates.
#CheckRunResults: {
	box?:     string @go(Box)
	mode?:    string @go(Mode) // "box" | "run"
	step?: [...#StepScore] @go(Step)
	summary!: #ScoreSummary @go(Summary)
}

// #StepScore — the scorer's verdict for one check:/agent-check: step, keyed by step id.
#StepScore: {
	id!:             string @go(ID)
	origin?:         string @go(Origin)
	text?:           string @go(Text)
	tag?: [...string] @go(Tag)
	keyword?:        string @go(Keyword)
	verb?:           string @go(Verb)
	status!:         string @go(Status)        // "pass" | "fail" | "skip" | "skipped"
	skipped_reason?: string @go(SkippedReason) // set when status=="skipped": "dep-unmet: <id>"
}

// #ScoreSummary — the pass/fail/skip tally block (the former hand-written TestRunSummary). The
// counts are Go `int` (type=int override — the former hand type; CUE `int` defaults to int64),
// so every existing ++/compare call site compiles unchanged.
#ScoreSummary: {
	total!: int @go(Total, type=int)
	pass!:  int @go(Pass, type=int)
	fail!:  int @go(Fail, type=int)
	skip!:  int @go(Skip, type=int)
}

// #CheckBedRequest — the transitional check-bed host-session seam (P12 Wave-2, K5-mortal).
// A compiled-in plugin-check drives the R10 bed sequence over HostBuild("cli"), but the
// lock/lease/env lifecycle + the node-derived bed shape are core state a separate module
// cannot hold: this op-discriminated envelope opens/drives/closes a host-side session keyed by
// Bed. Class-generic action noun "check-bed" (F11 — never a substrate/provider word). The
// setup/teardown pair are two of its ops; members-up/members-down/wait-ready are the
// mid-sequence host-coupled helpers (they run AFTER the substrate deploys, so cannot fold into
// setup, and call saveDeployState+libvirt+SSHExecutor/podman polls with no `charly` verb, so
// cannot be cli-reentry). DIES at K5 (post-loaderkit the plugin self-orchestrates its own flock
// via statekit, computes the repo-override itself, and calls the arbiter over InvokeProvider).
#CheckBedRequest: {
	op!:  string @go(Op)  // setup | members-up | members-down | wait-ready | teardown
	bed!: string @go(Bed) // the disposable bundle (bed) name — the session key
	ok?:  bool   @go(OK)  // teardown only: true → Lease.Release, false → Lease.ReleaseFailed
	dir?: string @go(Dir) // project dir (empty → host cwd), matching LoadUnified(dir)
}

// #CheckBedReply — the setup op returns the BedDescriptor (the node-derived shape the kind-blind
// plugin drives the sequence from — the substrate analogue of OpPrepareVenue's VenueDescriptor).
// All other ops return {} (errors ride the host-builder error return). PrereqSkip set ⇒ the bed
// is a clean SKIP (exit 3): the plugin writes the prereq-skip summary + returns CheckSkippedError,
// running NO other op (not even teardown — setup acquired nothing on the skip path).
#CheckBedReply: {
	calver?:      string            @go(Calver)                    // logDir calver (.check/<bed>/<calver>)
	log_dir?:     string            @go(LogDir)                    // host-relative; the plugin writes step logs here
	prereq_skip?: #CheckBedPrereqSkip @go(PrereqSkip, optional=nillable)
	// BedDescriptor — the substrate classification + refs the plugin drives from.
	is_vm?:       bool   @go(IsVM)
	is_local?:    bool   @go(IsLocal)
	is_group?:    bool   @go(IsGroup)
	is_external?: bool   @go(IsExternal) // in-place external (bundle-del teardown)
	image?:       string @go(Image)      // pod bed box ref ("" for vm/local/group)
	vm_template?: string @go(VMTemplate) // node.From for a vm bed (the ENTITY — `charly vm build` builds off it)
	bed_domain?:  string @go(BedDomain)  // per-deploy live domain identity (`charly vm create/destroy/start … --domain <this>`, post-P33)
	image_tag?:   string @go(ImageTag)   // per-RUN bed-scoped image tag (<bed-root>-<runCalver>); every `charly box build` + deploy in the run passes it as --tag, so concurrent beds building the SAME fixture image name never collide on the store-global short-name→newest-local-CalVer resolution (#75 — the tag analogue of bed_domain=deploy-name)
	local_ref?:   string @go(LocalRef)   // node.From for a local bed
	vm_domains?: [...string] @go(VMDomains)      // charly-<domain> set locked by setup (per-deploy, post-P33)
	check_live_refs?: [...string] @go(CheckLiveRefs) // bed + nested-child refs
	child_keys?: [...string] @go(ChildKeys)      // sortedNestedKeys(node.Children) — ALL nested children (pod path)
	// local_child_keys is the HOST-ROOTED (kind:local) subset of child_keys, in the same order. A VM
	// root deploys ONLY these host-side (mirroring the core deployNestedLocalChildren): a VM's
	// nested CONTAINER children are deployed in-guest by plugin-deploy-vm's PostApply, so a host-side
	// re-deploy would be wrong. The pod path uses child_keys (all); the vm path uses local_child_keys.
	local_child_keys?: [...string] @go(LocalChildKeys)
	// members carries each sibling member's build coordinates so a GROUP bed's plugin can drive the
	// per-member image build loop (`charly vm build <from>` / `charly box build <image>` + check box)
	// BEFORE the host `members-up` op deploys them (bringUpMembers assumes pre-built images).
	members?: [...#CheckBedMember] @go(Members)
	run_build?:   bool @go(RunBuild)   // check_level ≥ build
	run_runtime?: bool @go(RunRuntime) // check_level ≥ noagent
	run_agent?:   bool @go(RunAgent)   // check_level == agent
}

// #CheckBedMember — one sibling member's build coordinates (the group-bed member build loop).
#CheckBedMember: {
	key!:   string @go(Key)
	is_vm?: bool   @go(IsVM)  // vm member — build its disk via `charly vm build <from>` (bringUpMembers does vm create)
	image?: string @go(Image) // pod member box ref ("" for a vm member)
	from?:  string @go(From)  // vm member kind:vm entity (the build/spec source; entity-scoped, NOT --domain)
}

// #CheckBedPrereqSkip — a bed the host skips for an absent HOST prerequisite (a GPU resource
// whose vendor has no matching card): a clean SKIP (exit 3), not a failure.
#CheckBedPrereqSkip: {
	token!:  string @go(Token)
	vendor!: string @go(Vendor)
	reason!: string @go(Reason)
}

// #DeployCompileRequest is the per-node COMPILE seam (K4-B): the host computes the
// per-node selection (resolved box — authored OR synthetic — projected to a
// ResolvedBoxView; the FINAL pruned candy order; the host-side HostContext incl. the
// preresolved BuilderContext) and asks the command:bundle plugin's OpCompile handler to
// compile. The plugin fetches the resolved-project envelope itself via
// HostBuild("resolved-project") (the established seam — it does NOT receive the whole
// project in the request), re-hydrates the box vocab via deploykit.NewSpecResolvedBox and
// each candy model via deploykit.NewSpecCandyModel, loops deploykit.BuildDeployPlan over
// the host-provided order, and returns []InstallPlanView. The host re-materializes
// []*InstallPlan from the views via deploykit.PlanFromView.
//
// BoxView is the resolved box to compile against, INLINE (the host projects authored
// boxes via projectResolvedBox AND synthetics via syntheticHostBox/syntheticVmBox the SAME
// way — both become a spec.ResolvedBoxView). HostContextJSON is the marshalled
// deploykit.HostContext (MachineVenue/Distro/Glibc/BuilderImage + the preresolved
// BuilderContext map) — a hand-written sdk/deploykit type with no CUE def, so it rides as
// an opaque RawBody envelope (the VmJSON/PodConfigJSON idiom; the plugin unmarshals into
// deploykit.HostContext, which it imports via github.com/opencharly/sdk/deploykit). Tag is
// the image CalVer pin (for the plan Version field when set). Dir is the project dir the
// plugin threads into its HostBuild("resolved-project") call (empty → plugin cwd).
#DeployCompileRequest: {
	dir!:          string          @go(Dir)
	box_view!:     #ResolvedBoxView @go(BoxView)
	order?:        [...string]     @go(Order)
	host_context!: bytes           @go(HostContextJSON, type=RawBody)
	tag?:          string          @go(Tag)
}

// #DeployCompileReply is the OpCompile reply: the compiled plans as marshalled
// []spec.InstallPlanView (a hand-written sdk/spec wire type with no CUE def → opaque
// RawBody envelope; the host unmarshals into []spec.InstallPlanView and re-materializes
// []*spec.InstallPlan via deploykit.PlanFromView), plus the base identity (box name) and
// the resolved candy set (the order, for deployID + overlay-candy propagation).
#DeployCompileReply: {
	plans!:     bytes     @go(PlansJSON, type=RawBody)
	base?:      string    @go(Base)
	candy_set?: [...string] @go(CandySet)
}
