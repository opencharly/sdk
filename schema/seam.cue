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
// VmBuildCmd fields). The host resolves the kind:vm entity + build config +
// dispatches the per-source-kind VM-disk build engine itself (the engine stays
// core, exactly as the box-build engine stayed core behind HostBuild("image") in
// P8). The plugin's `charly vm build` command is THIN — it forwards these flags.
#VmBuildRequest: {
	box!:       string @go(Box)
	size?:      string @go(Size)
	root_size?: string @go(RootSize)
	tag?:       string @go(Tag)
	type?:      string @go(Type)
	transport?: string @go(Transport)
	console?:   bool   @go(Console)
}

// #VmBuildReply is the "vm-build" host-builder reply — empty; the build prints its
// own progress to the shared stdio and signals failure via the error return.
#VmBuildReply: {}

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
