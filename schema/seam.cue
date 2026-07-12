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
