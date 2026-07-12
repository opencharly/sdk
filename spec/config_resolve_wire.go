package spec

// config_resolve_wire.go — the host-resolved DATA a COMPILED-IN command plugin needs when its
// handler must act on the project's resolved config but cannot LoadUnified itself (a plugin has no
// project loader — the loader is a core Mechanism). The host runs the generic "config-resolve"
// F10 host-builder (registerHostBuilder), resolving the kind:vm entity + runtime settings ONCE and
// shipping the result back over the reverse channel. This is the COMMAND-time twin of the
// deploy-time LifecyclePrepareInput seam (deploy_wire.go): both resolve host-only config for a
// plugin that owns the behaviour. Its FIRST consumer is command:vm (candy/plugin-vm), whose
// create/build/import handlers need the resolved VmSpec + resources + backend + claimant; the pod
// (P11) and bundle (P13) command families reuse the SAME generic seam. The action noun is
// class-generic ("config-resolve" — resolve project config), never a substrate word (the F11
// uniform-API gate forbids a provider word on the host-builder surface).
//
// It embeds the resolved runtime objects DIRECTLY (*ResolvedVm, *ResolvedResource, *Deploy) — the
// same hand-written-wire pattern as LifecyclePrepareInput, because those types are hand-written
// runtime wire (not gengotypes output), so a CUE #ConfigResolveReply referencing them would
// generate colliding duplicate structs (SDD "the generator cannot express the shape" exception,
// kept in lockstep with its embedded siblings until that whole family migrates to CUE).

// ConfigResolveRequest asks the host to resolve the project config for one entity. Entity is the
// resolved entity name (a kind:vm entity for command:vm); Dir is the project dir (empty → the host
// uses its own cwd), matching the LoadUnified(dir) contract.
type ConfigResolveRequest struct {
	Entity string `json:"entity"`
	Dir    string `json:"dir,omitempty"`
}

// ConfigResolveReply is the host-resolved config data. For a kind:vm entity: VM is the resolved
// vm value envelope (uf.VM[entity] via resolveVmViaPlugin), Resources is the resolved resource map
// (uf.resolveResources() — drives GPU auto-allocation), Backend is the resolved vm backend
// (resolveVmBackend, which also starts the libvirt user session), and Claimant/ClaimantNode carry
// the exclusive-resource claimant (lookupVMClaimant) the handler acquires a preempt lease for.
// VmBackend/BuildEngine/RunEngine are the runtime-settings fields (ResolveRuntime) the create/build
// pipeline reads. Fields absent for an entity that does not need them stay zero.
type ConfigResolveReply struct {
	VM           *ResolvedVm                  `json:"vm,omitempty"`
	Resources    map[string]*ResolvedResource `json:"resources,omitempty"`
	Backend      string                       `json:"backend,omitempty"`
	Claimant     string                       `json:"claimant,omitempty"`
	ClaimantNode *Deploy                      `json:"claimant_node,omitempty"`
	VmBackend    string                       `json:"vm_backend,omitempty"`
	BuildEngine  string                       `json:"build_engine,omitempty"`
	RunEngine    string                       `json:"run_engine,omitempty"`
	// VmState is the entity's persisted deploy-ledger runtime state (instance-id, ssh_port, disk
	// path) — the READ half of the ledger dep. The host resolves it (loadDeployConfigForRead →
	// LookupKey "vm:<entity>") so the plugin's build/create handlers reuse the persisted auto-port
	// + regenerate the seed ISO with the prior state, without holding the deploy-config lock.
	VmState *VmDeployState `json:"vm_state,omitempty"`
	// VmEntities is the project's declared kind:vm entity NAMES (the keys of uf.VM) — the enumeration
	// `charly vm import` needs to detect name conflicts, which the entity-specific reply cannot give.
	VmEntities []string `json:"vm_entities,omitempty"`
}

// ConfigPersistRequest is the WRITE twin of config-resolve: a command plugin asks the host to
// persist (or remove) an entity's deploy-ledger entry. The host owns the ledger + its blocking
// acquireDeployConfigLock (a core Mechanism — the plugin is a separate module and MUST NOT hold a
// process-shared file lock across the boundary), so the plugin resolves its intent into this
// envelope and the host applies it under the lock. Key is the full deploy key ("vm:<name>");
// Remove deletes the entry (destroy), else Entity + VmState are saved (create persist-auto-port).
// The action noun "config-persist" is class-generic (F11); P11/P13 reuse it for their own state.
type ConfigPersistRequest struct {
	Key     string         `json:"key"`
	Entity  string         `json:"entity,omitempty"`
	VmState *VmDeployState `json:"vm_state,omitempty"`
	Remove  bool           `json:"remove,omitempty"`
}

// ConfigPersistReply is the config-persist host-builder reply — empty; the host-builder signals
// failure via its error return (surfaced to the plugin over the reverse channel).
type ConfigPersistReply struct{}
