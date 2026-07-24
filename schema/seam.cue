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

// #EnsureImageRequest asks the host to ensure an image is present locally — pulling a remote ref
// or building it locally as needed (K4: EnsureImagePresent, unchanged core logic — resolving a
// remote ref, pulling, or falling back to a local `charly box build`, all of which need the
// project loader / Config / the provider registry a plugin cannot hold). The two portable tiers
// (LocalImageExists / TransferImage) run plugin-side BEFORE this seam is reached; this covers only
// the "not present on either engine" cold-start fallback. Class-generic action noun "ensure-image"
// (F11 — never a substrate word); shared by every deploy substrate that resolves a runtime image
// (pod, vm's builder-image path), not pod-exclusive.
#EnsureImageRequest: {
	image_ref!:  string @go(ImageRef)
	run_engine!: string @go(RunEngine)
}

// #EnsureImageReply is empty on success — a non-nil host error means the image could not be made
// available (rides the RPC error, not a reply field).
#EnsureImageReply: {}

// #DeployOverlayRequest asks the host for the PER-HOST deploy-config overlay (K4:
// deploykit.LoadDeployConfigForRead — the runtime ledger at ~/.config/charly/charly.yml, NOT the
// project charly.yml the resolved-project envelope projects; Mode Purity keeps the two apart, same
// distinction #PodDisposableRequest documents). Unlike #PodDisposableRequest (a single overlay
// BIT), several pod-lifecycle resolvers need CROSS-DEPLOYMENT visibility (deploykit.BundleConfig's
// GlobalEnvForImage/OccupiedHostPorts/DeployedContainerNames all read OTHER deploys' entries, not
// just the caller's own), so a single-field extraction can't serve them — the host returns the
// WHOLE marshaled *deploykit.BundleConfig and the plugin calls the SAME already-portable
// deploykit methods locally. Re-fetched on EVERY call (no caching): the ledger can change between
// invocations (an intervening `charly config`), and PrepareVenue-time data is stale by the time
// OpStart/OpStop/OpShell run much later — this is NOT threaded through the one-shot
// LifecyclePrepareInput. Class-generic action noun "deploy-overlay" (F11 — never a substrate
// word); the SAME need config_image.go's eventual move has (R3 — one seam, not two).
#DeployOverlayRequest: {
	context!: string @go(Context) // caller label for host-side diagnostics, e.g. "charly start tunnel"
}

// #DeployOverlayReply carries the marshaled per-host BundleConfig. config_json is the JSON
// encoding of *deploykit.BundleConfig (nil-safe: absent/null when no per-host overlay file
// exists yet, matching LoadDeployConfigForRead's own nil-BundleConfig contract).
#DeployOverlayReply: {
	config_json?: bytes @go(ConfigJSON,type=RawBody)
}

// #DevicePatternsRequest is empty — the embedded device_patterns/gpu_vendors directives are
// baked into charly-core's binary (the embedded default charly.yml), not project- or host-scoped,
// so nothing varies per call. Asks the host for the tables candy/plugin-gpu's detect-host-devices
// action needs (K4: plugin-deploy-pod's device auto-detection reaching verb:gpu directly, the same
// dispatch charly-core's gpu_shim.go already does — mirrors candy/plugin-vm/vm_gpu_shim.go's
// existing InvokeProvider("verb","gpu",...) precedent). Class-generic action noun
// "device-patterns" (F11 — never a substrate word); any substrate resolving devices needs it.
#DevicePatternsRequest: {}

// #DevicePatternsReply carries the two embedded tables verbatim (see charly/devices.go).
#DevicePatternsReply: {
	device_patterns?: [...string] @go(DevicePatterns)
	gpu_vendors?: {[string]: string} @go(GpuVendors)
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

// #DeployTreeResolveRequest/#DeployTreeResolveReply — K4 lane A. candy/plugin-bundle now OWNS
// the `charly bundle add` dispatch CONTROL FLOW (Run's target-path resolve + the pre-order tree
// walk); resolveTreeRoot (reads LoadUnified, a core Mechanism the plugin cannot import) stays
// host-side, returning the WHOLE merged project+operator deploy tree so the plugin walks it
// itself via the already-pure sdk/deploykit WalkDeploymentTree/ResolveNodePath. Also connects the
// deployment's out-of-tree plugin candies (loadDeployPlugins) — the ONE per-invocation preamble
// every dispatch needs before ResolveTarget can route to an external substrate. root_venue_ssh
// reports whether the resolved root's stamped descent traits are the "ssh" venue (a vm root) —
// the plugin dispatches node-only in that case (nested pods deploy IN the guest), mirroring the
// prior in-core check without needing the registry-backed nodeTraits call itself.
#DeployTreeResolveRequest: {
	path!:      string @go(Path) // the target dotted path (Run's targetPath == c.Name)
	add_candy?: [...string] @go(AddCandy) // CLI --add-candy, threaded into loadDeployPlugins's scan
}
#DeployTreeResolveReply: {
	tree?:          {[string]: #Deploy} @go(Tree, type=map[string]*Deploy)
	root_venue_ssh?: bool                @go(RootVenueSSH)
}

// #DeployNodeDispatchRequest/#DeployNodeDispatchReply — the per-node `charly bundle add`
// terminal step (K4 lane A keystone, RDD-spike-proven): resolve+compile+ResolveTarget+Add for
// ONE tree position, reached once per node from the plugin's own walk instead of core walking
// in-process. ancestor_paths/ancestor_nodes let the host reconstruct the SAME parentExec chain
// the OLD in-core walk built (deriveChildExecutorForPath is pure Go over spec/kit types and is
// re-run HOST-side here) — a live DeployExecutor never needs to cross the wire.
//
// target/vm_entity (W4 pure-helpers relocation) are PRE-RESOLVED plugin-side (classifyNodeTarget
// / resolveVmEntity — both pure functions of node+path, now living in candy/plugin-bundle) and
// carried across the wire so the host-side dispatch no longer recomputes them: the host trusts
// target/vm_entity as sent (an empty vm_entity is itself a valid resolved value — "no vm entity
// applies to this node" — never a sentinel meaning "recompute me").
#DeployNodeDispatchRequest: {
	path!:  string @go(Path)
	node?:  #Deploy @go(Node, type=*Deploy)
	ancestor_paths?: [...string] @go(AncestorPaths)
	ancestor_nodes?: [...#Deploy] @go(AncestorNodes)
	ref?:                string @go(Ref)
	add_candy?: [...string] @go(AddCandy)
	tag?:                string @go(Tag)
	dry_run?:            bool   @go(DryRun)
	// node_only mirrors `charly bundle add --node-only`: threaded onto the resolved
	// *externalDeployTarget so its Add skips the substrate's PostApply (e.g. a vm's nested
	// target:pod children) — the walk itself already dispatches only this ONE node either way,
	// this flag additionally suppresses the SUBSTRATE's own post-apply fan-out.
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
	target?:             string @go(Target)
	vm_entity?:          string @go(VmEntity)
}
#DeployNodeDispatchReply: {}

// #ConstructStepRequest/#ConstructStepReply — the "construct-step" HostBuild seam (K5-A item 1,
// compile-seam ctx-threading): the ONE genuinely host-only piece of the former compileActOp —
// resolving a `run:` act op's `plugin:` word against the PROVIDER REGISTRY (a clause-M kernel
// mechanism, never reachable from a plugin directly) to decide whether it lowers into a typed
// InstallStep (TypedStepProvider.ConstructStep, an in-proc-only Go method today — no change),
// an ExternalPluginStep (an out-of-process executorInvoker verb), an ExternalStep (a class:step
// provider's declared StepContract), or the generic OpStep fallthrough. Everything ELSE
// compileActOp used to read off `layer`/`img` is ALREADY portable sdk/deploykit-side (CandyModel,
// *buildkit.ResolvedBox, deploykit.ResolveUserSpec) and never needs to cross this wire — the
// caller (deploykit.BuildDeployPlan, now ctx/exec-threaded) resolves those PORTABLE pieces
// itself and sends only the reduced scalars the registry-consult logic actually reads. The reply
// carries the constructed step as an OPAQUE #InstallStepView (StepToView/StepFromView, R3 — the
// SAME wire-view round-trip every other step-carrying seam uses) so the caller re-materializes a
// real deploykit.InstallStep without a second per-kind decode path. No new #Op selector is
// needed: the registry-consult decision is HostBuild-KIND-dispatched (string, like every other
// seam in this file), not a provider-targeted Invoke — compileActOp's logic runs UNCHANGED
// host-side inside the seam handler.
#ConstructStepRequest: {
	op!:               #Op    @go(Op)
	candy_name!:        string @go(CandyName)
	candy_source_dir?:  string @go(CandySourceDir)
	candy_vars?: {[string]: string} @go(CandyVars)
	resolved_user?:     string @go(ResolvedUser)
	pkg_format?:        string @go(PkgFormat)
	distro_tags?: [...string] @go(DistroTags)
}
#ConstructStepReply: {
	step?: #InstallStepView @go(Step, type=*InstallStepView)
}

// #DeployMembersRequest/#DeployMembersReply — bring up / tear down a deployment's sibling
// members (bringUpMembers/tearDownMembers — providerRegistry + ledger + subprocess-dependent,
// stays host-side), reached once at the end of Run() / the start of `charly bundle del`.
#DeployMembersRequest: {
	node?: #Deploy @go(Node, type=*Deploy)
}
#DeployMembersReply: {}

// #DeployDelResolveRequest/#DeployDelResolveReply — resolve a `charly bundle del` target's
// BundleNode (resolveDelNode: literal "host" / "vm:"-prefix legacy forms / a charly.yml tree
// entry / a ref-based pod-artifact probe) — needs LoadUnified + the on-disk artifact probe, so
// it stays host-side; the plugin's `charly bundle del` calls this FIRST.
#DeployDelResolveRequest: {
	name!: string @go(Name)
}
#DeployDelResolveReply: {
	node?: #Deploy @go(Node, type=*Deploy)
	kind?: string  @go(Kind)
}

// #DeployNodeDelDispatchRequest/#DeployNodeDelDispatchReply — the `charly bundle del` terminal
// step: ResolveTarget + target.Del, honoring the teardown gates (the prior "deploy-del"
// host-builder's tail, unchanged; the live ReverseRunner is still never carried on the wire — a
// programmatic teardown needing a specific runner is resolved host-side during dispatch).
#DeployNodeDelDispatchRequest: {
	name!:              string @go(Name)
	node?:              #Deploy @go(Node, type=*Deploy)
	assume_yes?:        bool   @go(AssumeYes)
	keep_repo_changes?: bool   @go(KeepRepoChanges)
	keep_services?:     bool   @go(KeepServices)
	keep_image?:        bool   @go(KeepImage)
	dry_run?:           bool   @go(DryRun)
}
#DeployNodeDelDispatchReply: {}

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

// #DeployConfigSaveRequest is the K4-C narrow seam for saveBundleConfigNodeForm — the
// `charly bundle import`/`reset` deploy-state WRITE step. command:bundle's show/export/status
// leaves moved to the plugin outright (deploykit.LoadBundleConfig/ExportAllBox/ParseDeployKey
// etc. are already sdk-portable, and export's project-load touch reuses the existing
// HostBuild("resolved-project") seam) — only the SAVE step still needs a seam: its per-entry
// marshal callback (marshalBundleNode, deploy_nodeform.go) resugars each plan step's internal
// plugin/plugin_input pair back to the authored `<word>: <input>` sugar via the host-owned
// pluginPrimaries registry (populated at compiled-in plugin init() + the byte-gated external
// prescan) — a live, in-process registry a separate-module plugin cannot reach directly.
// Config carries the marshalled deploykit.BundleConfig as an opaque RawBody envelope (a
// hand-written sdk/deploykit type with no CUE def, matching the DeployCompileRequest
// HostContextJSON idiom).
#DeployConfigSaveRequest: {
	config!: bytes @go(ConfigJSON, type=RawBody)
}

// #DeployConfigSaveReply is the "deploy-config-save" host-builder reply — empty (failure
// surfaces via the RPC error itself).
#DeployConfigSaveReply: {}

// #AndroidEntityResolution is the kind="android" payload carried OPAQUELY inside
// #DeployEntityResolveReply.entity (unit 6a): the resolved kind:android #ResolvedAndroid spec
// (CUE-sourced at schema/substrate_template.cue, SDD conversion — carried OPAQUELY here anyway,
// see the #DeployEntityResolveRequest doc below for why) PLUS the google-play credentials,
// resolved host-side (the credential STORE touch — DefaultCredentialStore — is core-only; the
// plugin never calls it directly, matching every other cutover's InvokeProvider-adjacent
// credential deferral).
#AndroidEntityResolution: {
	spec?:         bytes  @go(SpecJSON, type=RawBody)
	google_email?: string @go(GoogleEmail)
	google_token?: string @go(GoogleToken)
}

// #EphemeralRegisterRequest/#EphemeralRegisterReply — the host→command:bundle OpEphemeralRegister
// leg (FINAL/K5 unit 6a): ephemeral_lifecycle.go's cross-substrate ephemeral-instance registration
// (systemd TTL transient timer + parent-detection + charly.yml persistence) moved to
// candy/plugin-bundle, the substrate-neutral deploy-lifecycle owner (vm/pod/k8s all register
// through it via deploy_add_shared.go's registerEphemeralIfMarked, which STAYS host-side —
// candidate-floor sibling of bundle_add_cmd.go — and Invokes this as the FIRST action of every
// Add). Registration failure is best-effort (logged plugin-side, never fatal to the deploy) —
// the reply is empty; the host discards the returned handle (the prior in-core contract already
// did, registerEphemeralIfMarked only checked the error).
#EphemeralRegisterRequest: {
	name!: string  @go(Name)
	node!: #Deploy @go(Node, type=*Deploy)
}
#EphemeralRegisterReply: {}

// #DeployEntityResolveRequest/#DeployEntityResolveReply — the F6-family GENERIC host-side
// entity-lookup seam (unit 6a, extended for unit 6b's k3s_post/vm_backend_lifecycle consumers,
// and for W4's resolveNodeTemplate — candy/plugin-bundle's kind:local template lookup): a
// substrate PRERESOLVE body (k8s/vm/android, F6) OR a peer consumer resolving a cross-reference
// (k3s_post's deployVMForwards, vm_backend_lifecycle's vmConfiguredBackend, resolveNodeTemplate's
// kind:local merge) needs a LoadUnified-coupled lookup a plugin cannot do itself — EITHER (a) its
// own deploy-tree node by name (the Update-path re-resolve every preresolver does when node==nil,
// OR a bundle-key cross-reference's From-field hop — today: resolveTreeRoot) or (b) a referenced
// kind:<word> entity (k8s/android/vm/local) by name, returned as the WHOLE RESOLVED envelope so a
// caller just reads its fields (Backend, Network.PortForwards, Candy, …) without tracing the
// resolver's own portability (today: findK8sSpec / findAndroidSpec / a direct uf.VM[name] lookup +
// resolveVmViaPlugin / findLocalSpec). ONE discriminated request replaces per-purpose kinds:
// `kind` is DATA the host body dispatches on internally (clause-D) — never a compiled-in per-KIND
// HostBuild registration, so a new consumer needs no new wire shape, only a new `case` in the host
// handler (or reuse of an existing one — "bundle" and "deploy" share ONE case, both a deploy-tree
// node lookup by name). `entity` carries the kind-specific result OPAQUELY — ResolvedK8s/
// ResolvedAndroid/the vm entity (ResolvedVm)/ResolvedLocal are ALL CUE-sourced
// (schema/substrate_template.cue, schema/vm.cue; SDD conversion), but this seam still carries them
// as opaque bytes rather than a typed field, because `kind` is DATA the host dispatches on
// internally (clause-D) and the caller already knows which kind it asked for and decodes
// accordingly — mirroring the DeployCompileReply / DeployConfigSaveRequest RawBody idiom used
// throughout this file for the same reason. The "local" case's EMPTY reply (no EntityJSON, no
// error) is itself meaningful — "no kind:local template by that name" — distinct from a genuine
// host-side load-failure error; the caller (resolveNodeTemplate) tells the two apart.
#DeployEntityResolveRequest: {
	kind!: string @go(Kind) // "" | "deploy" | "bundle" for a deploy-tree node lookup (node.From carries a cross-ref hop); "k8s"|"android"|"vm"|"local" for a kind:<word> entity lookup (the WHOLE resolved envelope)
	name!: string @go(Name)
	dir?:  string @go(Dir)
}
#DeployEntityResolveReply: {
	node?:   #Deploy @go(Node, type=*Deploy)          // populated when kind=="" (deploy-tree lookup)
	entity?: bytes   @go(EntityJSON, type=RawBody)     // populated when kind!="" (opaque kind-specific entity)
}

// #EphemeralTeardownRequest/#EphemeralTeardownReply — the host→command:bundle
// OpEphemeralTeardown leg: TeardownEphemeralLifecycle's LAST-action-of-Del counterpart
// (recursive nested-child teardown, TTL timer cancel, snapshot/parent refcount decrement,
// charly.yml cleanup), called from every substrate's post-teardown hook (today:
// vm_lifecycle_preresolve.go's vmLifecyclePostTeardown).
#EphemeralTeardownRequest: {
	name!: string  @go(Name)
	node!: #Deploy @go(Node, type=*Deploy)
}
#EphemeralTeardownReply: {}

// #K8sGenerateKustomizeRequest/#K8sGenerateKustomizeReply — the "k8s-generate-kustomize"
// HostBuild seam (FINAL/K5 unit 6a): the deploy:k8s preresolve body (now plugin-side,
// candy/plugin-kube/preresolve.go) resolves the cluster template (via
// "deploy-entity-resolve", kind="k8s") + the image ref + capabilities itself (all
// sdk-portable — kit.ResolveLocalImageRef / deploykit.ExtractMetadata, no LoadUnified
// needed), then calls back HERE for the ONE genuinely core-only step:
// charly/k8s_generate.go's GenerateK8sKustomize (Invokes the compiled-in verb:k8sgen
// generator + the M16 egress gate + the disk I/O — all core-only glue, unchanged,
// STAYS in charly/ since `charly bundle from-box --target k8s`
// (k8s_deploy_from_box.go) is its OTHER, non-moving caller). Cluster/Capabilities ride
// opaque (the established RawBody idiom this file uses throughout for hand-written
// sdk/deploykit types with no CUE def — e.g. #PodConfigHookSecretEnvRequest.MetaJSON).
#K8sGenerateKustomizeRequest: {
	name!:       string @go(Name)
	image_ref!:  string @go(ImageRef)
	node!:       #Deploy @go(Node, type=*Deploy)
	caps!:       bytes  @go(CapsJSON, type=RawBody)    // opaque *Capabilities (spec.BoxMetadata)
	cluster!:    bytes  @go(ClusterJSON, type=RawBody) // opaque *ResolvedK8s
	output_dir?: string @go(OutputDir)
}
#K8sGenerateKustomizeReply: {
	overlay_path!: string @go(OverlayPath)
	tree_root!:    string @go(TreeRoot)
}

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

// #PodLifecyclePlan is the pod-lifecycle carrier (the K4 deep-body move). Formerly host-resolved
// and threaded on OpStart/OpStop; candy/plugin-deploy-pod now SELF-RESOLVES it (resolve.go's
// resolveStartQuadlet/resolveStopPlan, resolve_direct.go's resolveStartDirect) from the deploy key +
// the raw CLI opts (#PodStartOpts/#PodStopOpts), reaching the host only for genuinely host-only
// mechanisms (the deploy-overlay HostBuild seam, verb:credential, verb:enc, verb:tunnel) — this type
// is now built AND consumed entirely within the plugin process. It EXECUTES it — running the
// container start/stop over the served host executor and composing enc + tunnel via
// InvokeProvider(verb:enc/verb:tunnel), so the former podCli("start"/"stop"/…) `charly`-reentries
// are DELETED (bodies, not shells). The pre-built enc
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
	tunnel?:  #TunnelConfig @go(Tunnel,optional=nillable) // resolved tunnel config (nil ⇒ no tunnel) — driven via podTunnelOp(ctx,exec,"start",...)@start / podTunnelOp(ctx,exec,"stop",...)@stop, both verb:tunnel over InvokeProvider
}

// #PodStartOpts carries `charly start`'s direct-mode CLI extras (K4 inversion, quadlet-mode
// first): the plugin now SELF-RESOLVES the #PodLifecyclePlan (over the deploy-overlay HostBuild
// seam + the already-portable sdk resolvers) using these opts + the deploy key already on
// lifecycleParams.Name — replacing the former host-side resolvePodStartPlan. The quadlet path
// ignores every field (mirrors the pre-inversion contract — CLI extras apply only to direct mode).
#PodStartOpts: {
	env?:            [...string] @go(Env)
	env_file?:       string      @go(EnvFile)
	port?:           [...string] @go(Port)
	volume_flag?:    [...string] @go(VolumeFlag)
	bind?:           [...string] @go(Bind)
	no_auto_detect?: bool        @go(NoAutoDetect)
}

// #PodStopOpts carries `charly stop --unmount` (K4 inversion): the plugin self-resolves the STOP
// #PodLifecyclePlan using this + the deploy key, replacing the former host-side resolvePodStopPlan.
#PodStopOpts: {
	unmount?: bool @go(Unmount)
}

// #PodLiveStdioPlan is the F12 LIVE-STDIO carrier — ONE carrier for shell + cmd + logs (identical
// shape, R3; the op + the executor method distinguish them). Formerly host-resolved and threaded on
// OpAttach/OpLogs op.Params; candy/plugin-deploy-pod now SELF-RESOLVES it (resolve_f12.go's
// resolveAttachPlan/resolveShellPlan/resolveCmdPlan/resolveLogsPlan) from the deploy key +
// #PodAttachOpts/#PodLogsOpts, so this type is now built AND consumed entirely within the plugin
// process — no wire crossing for the plan itself, only for the opts that drive it. The plugin
// EXECUTES it over the served host executor via exec.RunInteractive (OpAttach — inherited LIVE
// stdin/stdout/stderr; the child `podman exec -it`/`-i` owns the PTY + resize + Ctrl-C) /
// exec.RunStream (OpLogs — inherited LIVE stdout/stderr, no stdin). UNARY: the host reverse-server
// runs IN the charly process, so os.Stdin/os.Stdout = the operator's terminal — stdio NEVER crosses
// the wire, only the session exit code (the hostBuildCli doctrine). This takes the F12 exit for the
// shell/cmd/logs-follow rows of the #57 M-core register: the former inline `charly shell`/`cmd`
// bodies + the podCli("logs") reentry are DELETED (bodies, not shells).
#PodLiveStdioPlan: {
	// resolved venue command:
	//   shell → `podman exec -it charly-<box> bash [-c cmd]` OR the ephemeral `podman run --rm -it … bash`
	//   cmd   → `<engine> exec [-e env] charly-<box>[-<sidecar>] sh -c <command>` (no -t; stdin piped)
	//   logs  → `<engine> logs [-f] [-n N] charly-<box>` OR quadlet `journalctl --user -u <svc> [-f] [-n N]`
	script!: string @go(Script)
}

// #PodShellOpts carries `charly shell`'s per-invocation CLI extras (K4/F12 inversion): the plugin
// self-resolves the #PodLiveStdioPlan using these + the deploy key, replacing the former host-side
// resolvePodShellPlan/buildShellArgs/buildExecArgs. interactive/wrap_pty are HOST-RESOLVED booleans
// (interactive = force_tty || isTerminal(); wrap_pty = force_tty && !isTerminal()) — the plugin is a
// subprocess whose own stdout is not the operator's terminal, so the tty check MUST happen host-side
// at the moment of the real CLI invocation and cross the wire as data, never be re-derived
// plugin-side.
#PodShellOpts: {
	tag?:            string      @go(Tag)
	env_file?:       string      @go(EnvFile)
	env?:            [...string] @go(Env)
	volume_flag?:    [...string] @go(VolumeFlag)
	bind?:           [...string] @go(Bind)
	no_auto_detect?: bool        @go(NoAutoDetect)
	interactive!:    bool        @go(Interactive)
	wrap_pty!:       bool        @go(WrapPTY)
}

// #PodCmdOpts carries `charly cmd`'s per-invocation extra (--sidecar), replacing the former
// host-side resolvePodCmdPlan.
#PodCmdOpts: {
	sidecar?: string @go(Sidecar)
}

// #PodAttachOpts carries the F12 Attach op's parameters (K4/F12 inversion): tty selects the shell
// resolver (interactive `charly shell`) vs the cmd resolver (`charly cmd`'s non-interactive `-i`
// exec) — mirrors the former host-side resolvePodAttachPlan dispatch, now run plugin-side.
#PodAttachOpts: {
	cmd?:      [...string]    @go(Cmd)
	tty!:      bool           @go(Tty)
	shell?:    #PodShellOpts  @go(Shell)
	cmd_opts?: #PodCmdOpts    @go(CmdOpts)
}

// #PodLogsOpts carries `charly logs [-f]`'s parameters (K4/F12 inversion), replacing the former
// host-side resolvePodLogsPlan. Mirrors charly-core's substrate-agnostic LogsOpts (Follow/Tail/
// Sidecar) — a plugin-facing wire twin, since LogsOpts itself is a hand-written charly-core type
// with no CUE def.
#PodLogsOpts: {
	follow?:  bool   @go(Follow)
	tail?:    int    @go(Tail,type=int)
	sidecar?: string @go(Sidecar)
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
// (FormatStepResults*) with byte-parity across every --format. A live `cue exp
// gengotypes` spike (P12) proved kit.CheckResult AS A WHOLE is genuinely inexpressible in
// CUE — its engine-internal `DeadlineExceeded bool json:"-"` field has no gengotypes
// construct — but confirmed the REST of the type (Op/Verb/Status/Message/Elapsed/
// Attempts/TotalElapsed/CapturedValue) generates faithfully. FLOOR-SLIM Unit 4 acted on
// that finding: #CheckResult (checkresult.cue) is the CUE-sourced base (→ spec.CheckResult),
// and kit.CheckResult is now `struct { spec.CheckResult; DeadlineExceeded bool
// json:"-" }` — an EMBEDDING wrapper, not a hand-duplicated type. So StepResult's JSON
// output still rides kit's Go marshal (embedding flattens transparently), but the
// exception the wire mandate's spike-proven path authorizes is now narrowed to EXACTLY
// the one field that forced it, not the whole type.
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

// #CheckLoadPluginsRequest asks the host to connect the out-of-process plugin candies a check
// plan's verb words reference (K1-unblock wave — the "live" check-run arm). Verb dispatch itself
// crosses the wire generically via InvokeProvider (S1 — command:check's pluginVerbResolver), but
// InvokeProvider only resolves an ALREADY-CONNECTED provider (or a compiled-in one); connecting an
// out-of-process candy is the plugin-loading M-mechanism (the kernel/plugin boundary law's clause
// M — plugin discovery/loading/connect stays core), so this seam is the entry point a plugin calls
// BEFORE dispatching a plan whose verbs may need an out-of-process candy connected. The host runs
// the UNCHANGED core engine (LoadConfig + resolveCheckRunnerContext: ScanAllCandyWithConfigOpts +
// collectReferencedPluginWords + loadProjectPlugins) as a pure SIDE EFFECT on its own
// providerRegistry — every subsequent InvokeProvider call in this same `charly check run`
// invocation then resolves. Class-generic action noun "check-load-plugins" (F11 — never a
// substrate/provider word).
#CheckLoadPluginsRequest: {
	name!: string @go(Name) // the deploy/bed name whose plan drives the reference scan
	dir?:  string @go(Dir)  // project dir (empty -> host cwd), matching LoadUnified(dir)
}

// #CheckLoadPluginsReply is empty on success — connect failures are best-effort WARNINGS on the
// host (mirroring resolveCheckRunnerContext's existing behavior: an unresolvable plugin fails
// loudly later, at actual verb dispatch, never here).
#CheckLoadPluginsReply: {}

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

// #PodStartRequest carries the `charly start` command flags (the former StartCmd's authored
// fields, DEPLOY-wave CLI-struct port). The command:pod plugin owns the CLI GRAMMAR but cannot
// drive the LifecycleTarget dispatch (ResolveTarget, the plugin loader — core Mechanisms), so
// `charly start`'s command is THIN — it forwards these flags to HostBuild("pod-start"), and the
// host runs the existing startViaLifecycle orchestration VERBATIM, exactly as `charly bundle add`
// stayed core behind HostBuild("deploy-add").
#PodStartRequest: {
	box!:            string @go(Box)
	tag?:             string @go(Tag)
	build?:           bool   @go(Build)
	env?: [...string] @go(Env)
	env_file?:        string @go(EnvFile)
	instance?:        string @go(Instance)
	port?: [...string] @go(Port)
	volume_flag?: [...string] @go(VolumeFlag)
	bind?: [...string] @go(Bind)
	no_autodetect?:   bool @go(NoAutoDetect)
}

// #PodStartReply is the "pod-start" host-builder reply — empty; the start prints its own
// progress to the shared stdio (the compiled-in plugin's HostBuild runs in charly's own process)
// and signals failure via the error return.
#PodStartReply: {}

// #PodStopRequest carries the `charly stop` command flags (the former StopCmd's authored fields).
// Forwarded to HostBuild("pod-stop"), which runs the existing stopViaLifecycle orchestration
// VERBATIM.
#PodStopRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
	unmount?:  bool   @go(Unmount)
}

// #PodStopReply is the "pod-stop" host-builder reply — empty, mirroring #PodStartReply.
#PodStopReply: {}

// #PodLogsRequest carries the `charly logs` command flags (the former LogsCmd's authored
// fields). Forwarded to HostBuild("pod-logs"), which runs the existing dispatchLifecycleTarget +
// LifecycleTarget.Logs orchestration VERBATIM (F12 — the host resolves the journalctl/`<engine>
// logs` stream command, the owning plugin streams it live to the operator's stdio).
#PodLogsRequest: {
	box!:      string @go(Box)
	follow?:   bool   @go(Follow)
	instance?: string @go(Instance)
	sidecar?:  string @go(Sidecar)
}

// #PodLogsReply is the "pod-logs" host-builder reply — empty, mirroring #PodStartReply.
#PodLogsReply: {}

// #PodRemoveRequest carries the `charly remove` command flags (the former RemoveCmd's authored
// fields). Forwarded to HostBuild("pod-remove"), which runs the existing remove orchestration
// VERBATIM (quadlet/companion-service teardown, pre_remove hooks, purge, deploy-entry cleanup —
// deeply core-type-coupled: BoxMetadata/ExtractMetadata/sidecar resolution/deploykit.
// CleanDeployEntry — not registry-bound, but not portable either).
#PodRemoveRequest: {
	box!:         string @go(Box)
	instance?:    string @go(Instance)
	purge?:       bool   @go(Purge)
	keep_deploy?: bool   @go(KeepDeploy)
	env?: [...string] @go(Env)
}

// #PodRemoveReply is the "pod-remove" host-builder reply — empty, mirroring #PodStartReply.
#PodRemoveReply: {}

// #PodShellRequest carries the `charly shell` command flags (the former ShellCmd's authored
// fields). Forwarded to HostBuild("pod-shell"), which runs the existing dispatchLifecycleTarget +
// LifecycleTarget.Attach orchestration VERBATIM (F12 — the host resolves the venue command, the
// owning plugin runs it over the served venue executor via RunInteractive, stdio host-held).
#PodShellRequest: {
	box!:            string @go(Box)
	tag?:             string @go(Tag)
	command?:         string @go(Command)
	build?:           bool   @go(Build)
	tty?:             bool   @go(TTY)
	env?: [...string] @go(Env)
	env_file?:        string @go(EnvFile)
	instance?:        string @go(Instance)
	volume_flag?: [...string] @go(VolumeFlag)
	bind?: [...string] @go(Bind)
	no_autodetect?:   bool @go(NoAutoDetect)
}

// #PodShellReply is the "pod-shell" host-builder reply — empty, mirroring #PodStartReply.
#PodShellReply: {}

// #PodServiceRequest carries the FULLY plugin-resolved argv for `charly service
// start/stop/status/restart` (Cutover B unit 2 completion): the plugin now performs
// resolveServiceInit/validateServiceName/execInitCommand's argv-building itself (all portable —
// spec.ResolvedInit is already an sdk alias, buildkit.RenderTemplate is sdk-portable) and sends
// the FINAL `<engine> exec <container> <tool> <op> [svc]` argv; the host does ONLY the
// irreducible dispatchLifecycleTarget + LifecycleTarget.Shell step (host_build_pod_lifecycle_dispatch.go's
// hostBuildPodService), mirroring start/stop/logs/update exactly.
#PodServiceRequest: {
	box!:      string      @go(Box)
	instance?: string      @go(Instance)
	argv!:     [...string] @go(Argv)
}

// #PodServiceReply is the "pod-service" host-builder reply — empty, mirroring #PodStartReply.
#PodServiceReply: {}

// #PodConfigSetupRequest carries the `charly config [setup]` command flags (the former
// BoxConfigSetupCmd's authored fields, PLUS explicit_ref — bundle_from_box_cmd.go's
// programmatically-set source-less-deploy field, below). P13-KERNEL direction-flip: forwarded
// from HostBuild("pod-config-setup") (host_build_pod_config.go's hostBuildPodConfigSetup) onward
// to the deploy:pod plugin's sdk.OpConfigSetup — the plugin now RUNS the former runConfig
// orchestration (candy/plugin-deploy-pod/config_setup.go), calling back the narrow
// "pod-config-*" seams below for the host/loader/registry/credential-coupled sub-steps.
#PodConfigSetupRequest: {
	box?:              string @go(Box)
	tag?:              string @go(Tag)
	build?:            bool   @go(Build)
	env?: [...string] @go(Env)
	clean?:            bool   @go(Clean)
	env_file?:         string @go(EnvFile)
	instance?:         string @go(Instance)
	port?: [...string] @go(Port)
	keep_mounted?:     bool   @go(KeepMounted)
	password?:         string @go(Password)
	refresh_secret?: [...string] @go(RefreshSecret)
	volume_flag?: [...string] @go(VolumeFlag)
	bind?: [...string] @go(Bind)
	encrypt?: [...string] @go(Encrypt)
	memory_max?:       string @go(MemoryMax)
	memory_high?:      string @go(MemoryHigh)
	memory_swap_max?:  string @go(MemorySwapMax)
	cpus?:             string @go(Cpus)
	seed?:             bool   @go(Seed)
	force_seed?:       bool   @go(ForceSeed)
	data_from?:        string @go(DataFrom)
	update_all?:       bool   @go(UpdateAll)
	ssh_key?:          string @go(SshKey)
	sidecar?: [...string] @go(Sidecar)
	list_sidecars?:    bool   @go(ListSidecars)
	no_autodetect?:    bool   @go(NoAutoDetect)
	// explicit_ref is set programmatically (never authored) by `charly bundle from-box`'s
	// source-less deploy path (bundle_from_box_cmd.go) — the P13-KERNEL direction-flip carries
	// it across the wire now that the ORCHESTRATION (formerly reading the kong:"-" Go field
	// directly) moved into the plugin.
	explicit_ref?: string @go(ExplicitRef)
}

// #PodConfigSetupReply is the "pod-config-setup" host-builder reply — empty, mirroring
// #PodStartReply.
#PodConfigSetupReply: {}

// #PodConfigStatusRequest carries `charly config status`'s flags. Forwarded to
// HostBuild("pod-config-status"), which runs the existing encStatus(box,instance) call VERBATIM.
#PodConfigStatusRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
}

// #PodConfigStatusReply is the "pod-config-status" host-builder reply — empty.
#PodConfigStatusReply: {}

// #PodConfigMountRequest carries `charly config mount`'s flags. Forwarded to
// HostBuild("pod-config-mount"), which runs the existing encMount(box,instance,volume) call
// VERBATIM.
#PodConfigMountRequest: {
	box!:      string @go(Box)
	volume?:   string @go(Volume)
	instance?: string @go(Instance)
}

// #PodConfigMountReply is the "pod-config-mount" host-builder reply — empty.
#PodConfigMountReply: {}

// #PodConfigUnmountRequest carries `charly config unmount`'s flags. Forwarded to
// HostBuild("pod-config-unmount"), which runs the existing encUnmount(box,instance,volume) call
// VERBATIM.
#PodConfigUnmountRequest: {
	box!:      string @go(Box)
	volume?:   string @go(Volume)
	instance?: string @go(Instance)
}

// #PodConfigUnmountReply is the "pod-config-unmount" host-builder reply — empty.
#PodConfigUnmountReply: {}

// #PodConfigPasswdRequest carries `charly config passwd`'s flags. Forwarded to
// HostBuild("pod-config-passwd"), which runs the existing encPasswd(box,instance) call VERBATIM.
#PodConfigPasswdRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
}

// #PodConfigPasswdReply is the "pod-config-passwd" host-builder reply — empty.
#PodConfigPasswdReply: {}

// #PodConfigRemoveRequest carries `charly config remove`'s flags (the former
// BoxConfigRemoveCmd's authored fields — distinct from `charly remove`/#PodRemoveRequest, which
// tears down the whole deploy; this removes only the quadlet + disables the service). Forwarded
// to HostBuild("pod-config-remove"), which runs the existing remove orchestration VERBATIM.
#PodConfigRemoveRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
}

// #PodConfigRemoveReply is the "pod-config-remove" host-builder reply — empty.
#PodConfigRemoveReply: {}

// P13-KERNEL step-4 direction-flip: BoxConfigSetupCmd/BoxConfigRemoveCmd's BODY (the former
// runConfig orchestration + updateAllDeployedQuadlets + the config_secret_migration.go pair)
// moved OUT of charly core INTO candy/plugin-deploy-pod (Ops sdk.OpConfigSetup/OpConfigRemove on
// the deploy:pod provider's Invoke — dispatched from host_build_pod_config.go's
// hostBuildPodConfigSetup/hostBuildPodConfigRemove, which now FORWARD onward via the SAME
// InvokeWithExecutor primitive InvokeProvider/grpcSubstrateLifecycle already use, instead of
// running the orchestration in-core). The plugin runs the ported logic and calls back these
// NARROW seams for the pieces that are genuinely host/loader/registry-coupled — the ledger's
// "FINAL/K5 IOU REGISTER" already registers the credential-store/enc.go family as
// registry-coupled, deliberately deferred inventory (NOT re-designed this wave); these seams wrap
// the EXISTING core functions VERBATIM, unchanged internally. BoxConfigStatusCmd/MountCmd/
// UnmountCmd/PasswdCmd do NOT move — they are already one-line forwards to enc.go (itself
// FINAL/K5-deferred), nothing to port.

// #PodConfigEnsureImageRequest: EnsureImage + ExtractMetadata bundle (registry/podman-store
// coupled — a plugin cannot resolve the local podman image store namespace itself).
#PodConfigEnsureImageRequest: {
	image_ref!:    string @go(ImageRef)
	build_engine!: string @go(BuildEngine)
}
#PodConfigEnsureImageReply: {
	meta_json!: bytes @go(MetaJSON, type=RawBody) // marshalled *spec.BoxMetadata
}

// #PodConfigResolveRefRequest: resolveDeployBoxName/resolveDeployResolvedImage/
// resolveShellImageRef bundle — reads the per-host charly.yml overlay + local podman image
// labels (loader + podman-store coupled).
#PodConfigResolveRefRequest: {
	box!:          string @go(Box)
	instance?:     string @go(Instance)
	tag?:          string @go(Tag)
	explicit_ref?: string @go(ExplicitRef)
}
#PodConfigResolveRefReply: {
	deploy_box_name!: string @go(DeployBoxName)
	image_ref!:       string @go(ImageRef)
}

// #PodConfigLoadDeployRequest / Reply: deploykit.LoadDeployConfigForRead(caller) — the
// per-host charly.yml Bundle map. Genuinely loader-coupled: deploykit.SaveBundleConfig/
// LoadDeployConfigForRead resolve through the package-var DeployStateHost seam, which is
// filled ONLY in the charly-core process's init() (charly/deploy_state_host.go) — an
// out-of-process plugin calling these directly would silently no-op (the kit's
// documented nil-safe degradation), so every load/save call site is a host seam, reusable
// across the whole ported flow.
#PodConfigLoadDeployRequest: {
	caller!: string @go(Caller)
}
#PodConfigLoadDeployReply: {
	config_json?: bytes @go(ConfigJSON, type=RawBody) // marshalled *deploykit.BundleConfig; absent ⇒ nil
}

// #PodConfigSaveBundleRequest / Reply: saveBundleConfigNodeForm(dc) — persists a (plugin-mutated)
// *deploykit.BundleConfig back through the SAME loader-coupled seam.
#PodConfigSaveBundleRequest: {
	config_json!: bytes @go(ConfigJSON, type=RawBody)
}
#PodConfigSaveBundleReply: {}

// #PodConfigLoadBundleReply: deploykit.LoadBundleConfig() — the whole-project Bundle map (no
// per-deploy-key focus), used by updateAllDeployedQuadlets's cross-deploy loop.
#PodConfigLoadBundleReply: {
	config_json?: bytes @go(ConfigJSON, type=RawBody)
}

// #PodConfigMigrateSecretsRequest / Reply: MigratePlaintextEnvSecret(dc, meta, box, instance) —
// the one-time plaintext-env → credential-store migration (file backup + DefaultCredentialStore
// + saveBundleConfigNodeForm, all FINAL/K5-deferred registry-coupled inventory per the ledger).
// config_json carries the ALREADY-LOADED dc (from #PodConfigLoadDeployRequest) so the host
// mutates + re-saves the SAME loaded structure the plugin is mid-flow with, never a stale reload.
#PodConfigMigrateSecretsRequest: {
	config_json!: bytes  @go(ConfigJSON, type=RawBody)
	meta_json!:   bytes  @go(MetaJSON, type=RawBody)
	box!:         string @go(Box)
	instance?:    string @go(Instance)
}
#PodConfigMigrateSecretsReply: {
	config_json!: bytes @go(ConfigJSON, type=RawBody) // the (possibly) updated dc
	migrated?:    int   @go(Migrated, type=int)
}

// #PodConfigScrubCliEnvRequest / Reply: scrubSecretCLIEnv(cliEnv, meta) — the credential-store
// Set() pre-scrub for `-e NAME=VAL` flags declared secret_accepts/secret_requires.
#PodConfigScrubCliEnvRequest: {
	cli_env?:   [...string] @go(CliEnv)
	meta_json!: bytes       @go(MetaJSON, type=RawBody)
}
#PodConfigScrubCliEnvReply: {
	cleaned?:  [...string] @go(Cleaned)
	imported?: int         @go(Imported, type=int)
}

// #PodConfigDetectDevicesRequest / Reply: DetectHostDevices()+LogDetectedDevices() —
// registry-coupled (DetectHostDevices resolves+Invokes verb:gpu via the host provider registry,
// which a peer plugin cannot dial without the InvokeProvider rewrite this family defers).
#PodConfigDetectDevicesRequest: {
	no_auto_detect?: bool @go(NoAutoDetect)
	// engine, when set to "podman" alongside a GPU detection, triggers EnsureCDI() (the pod
	// lifecycle's resolvePodRuntimeImage step) — bundled into this SAME seam call (R3) rather
	// than a dedicated one.
	engine?: string @go(Engine)
}
#PodConfigDetectDevicesReply: {
	detected_json!: bytes @go(DetectedJSON, type=RawBody) // marshalled DetectedDevices (= spec.DetectedDevices)
}

// #PodConfigTunnelResolveRequest / Reply: TunnelConfigFromMetadata(meta) — resolves the tunnel
// config (charly.yml overlay applied) from image labels.
#PodConfigTunnelResolveRequest: {
	meta_json!: bytes @go(MetaJSON, type=RawBody)
}
#PodConfigTunnelResolveReply: {
	tunnel_json?: bytes @go(TunnelJSON, type=RawBody) // marshalled *TunnelConfig; absent ⇒ nil
}

// #PodConfigResolveSidecarsRequest / Reply: the sidecar resolve+secret-provision bundle
// (embeddedSidecarBodies' go:embed data lives ONLY in the charly binary, not the plugin binary;
// resolveSidecarsViaPlugin + the sidecar-secret ProvisionPodmanSecrets loop are registry/
// credential-coupled per the same FINAL/K5 family).
#PodConfigResolveSidecarsRequest: {
	deploy_sidecars_json?: bytes    @go(DeploySidecarsJSON, type=RawBody) // map[string]json.RawMessage
	project_templates_json?: bytes  @go(ProjectTemplatesJSON, type=RawBody)
	cli_env?: [...string] @go(CliEnv)
	box!:      string @go(Box)
	instance?: string @go(Instance)
	run_engine!: string @go(RunEngine)
	auto_gen!:   bool   @go(AutoGen)
	refresh_secret?: [...string] @go(RefreshSecret)
}
#PodConfigResolveSidecarsReply: {
	persist_overrides_json?:  bytes @go(PersistOverridesJSON, type=RawBody)
	resolved_sidecars_json?:  bytes @go(ResolvedSidecarsJSON, type=RawBody)
	app_env?: [...string] @go(AppEnv)
	extra_env?: [...string] @go(ExtraEnv) // fallback env from sidecar secret provisioning
}

// #PodConfigProvisionSecretsRequest / Reply: CollectSecretsFromLabels + CollectCandySecretAccepts
// + ApplySecretRefresh + ProvisionPodmanSecrets + resolveSecretBackend bundle — the credential-
// store/podman-secret provisioning family (FINAL/K5-deferred, wrapped verbatim).
#PodConfigProvisionSecretsRequest: {
	meta_json!:  bytes  @go(MetaJSON, type=RawBody)
	box!:        string @go(Box)
	instance?:   string @go(Instance)
	run_engine!: string @go(RunEngine)
	auto_gen!:   bool   @go(AutoGen)
	refresh_secret?: [...string] @go(RefreshSecret)
}
#PodConfigProvisionSecretsReply: {
	provisioned_json?:  bytes @go(ProvisionedJSON, type=RawBody) // []deploykit.ProvisionedSecret
	fallback_env?: [...string] @go(FallbackEnv)
	resolutions_json?:  bytes @go(ResolutionsJSON, type=RawBody) // []SecretResolution
	is_keyring?: bool @go(IsKeyring)
}

// #PodConfigEncMountsRequest / Reply: ensureEncryptedMounts + (optional) encUnmount — the
// gocryptfs FUSE mount lifecycle (FINAL/K5-deferred registry-coupled family per the enc.go
// header: encExecViaPlugin + resolveEncPassphrase* route through the host provider registry +
// DefaultCredentialStore, neither portable without the InvokeProvider rewrite this family
// defers). This is the "ONE narrow credential seam" the standing ruling names.
#PodConfigEncMountsRequest: {
	box!:          string @go(Box)
	instance?:     string @go(Instance)
	auto_gen!:     bool   @go(AutoGen)
	keep_mounted!: bool   @go(KeepMounted)
}
#PodConfigEncMountsReply: {}

// #PodConfigInjectEnvProvidesRequest / Reply: injectEnvProvides(box,instance,envProvides,portMap)
// — loader-coupled (LoadDeployConfigForWrite + SaveBundleConfig internally).
#PodConfigInjectEnvProvidesRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
	env_provides?: {[string]: string} @go(EnvProvides)
	port_map_json?: bytes @go(PortMapJSON, type=RawBody) // marshalled map[int]int
}
#PodConfigInjectEnvProvidesReply: {
	changed?: bool @go(Changed)
}

// #PodConfigInjectMCPProvidesRequest / Reply: injectMCPProvides(box,instance,mcpProvides,portMap).
#PodConfigInjectMCPProvidesRequest: {
	box!:               string @go(Box)
	instance?:          string @go(Instance)
	mcp_provides_json?: bytes  @go(MCPProvidesJSON, type=RawBody) // marshalled []spec.MCPServerYAML
	port_map_json?:     bytes  @go(PortMapJSON, type=RawBody)
}
#PodConfigInjectMCPProvidesReply: {
	changed?: bool @go(Changed)
}

// #DeployConfigSaveStateRequest / Reply: deploykit.SaveDeployState(box,instance,input,
// marshalDeployNode) — the terminal per-deploy persist. input_json is the marshalled
// deploykit.SaveDeployStateInput (a hand-written sdk/deploykit type with no CUE def — the
// RawBody idiom, matching #PodConfigWriteRequest.pod_config_json). Renamed substrate-neutral
// (S3b, Q2): the seam started pod-only (candy/plugin-deploy-pod's config_setup.go/resolve.go)
// but candy/plugin-bundle's generic Add/Update apply body (deploy_target.go's
// persistDeployState) is now a THIRD caller across every substrate (pod/vm/local/k8s/android),
// so the "pod-config-*" family naming no longer fit — the underlying "deploy-config-save-state"
// host-builder kind renamed to match (was "pod-config-save-deploy-state").
#DeployConfigSaveStateRequest: {
	box!:        string @go(Box)
	instance?:   string @go(Instance)
	input_json!: bytes  @go(InputJSON, type=RawBody)
}
#DeployConfigSaveStateReply: {}

// #ArbiterBracketAcquireRequest / Reply — the K4-exit HostBuild seam for the acquire half of the
// Q1 resource-arbiter bracket (FLOOR-SLIM-proper Unit-8; the former core-resident
// charly/arbiter_bracket.go's arbiterBracketedStart, deleted — command:bundle's
// handleLifecycleSimple now calls this seam itself around its own Start dispatch, instead of core
// bracketing the dispatch call inline). `name` is the claimant identity; `node` carries the claim
// fields (requires_exclusive:/requires_shared:) acquireResourceForClaimant reads. The actual
// os.Setenv(CHARLY_PREEMPT_LEASE) STILL runs host-side (preserving the nested-`charly`-subprocess
// env-inheritance skip) — only OWNERSHIP of WHEN to call it moves plugin-side.
#ArbiterBracketAcquireRequest: {
	name!: string  @go(Name)
	node!: #Deploy @go(Node)
}
#ArbiterBracketAcquireReply: {}

// #ArbiterBracketReleaseRequest / Reply — the release half (arbiterBracketedStart's
// release-on-failure leg + arbiterBracketedStop's unconditional release-after-stop leg, both now
// reached from command:bundle's handleLifecycleSimple via this ONE seam).
#ArbiterBracketReleaseRequest: {
	name!: string @go(Name)
}
#ArbiterBracketReleaseReply: {}

// #PodConfigCleanDeployEntryRequest / Reply: deploykit.CleanDeployEntry(box, instance,
// marshalDeployNode) — the `charly remove` deploy-entry cleanup (Cutover B unit 2 remove-verb
// completion). Mirrors #DeployConfigSaveStateRequest's shape ({box!, instance?} → {}, the host
// owns the entire load+lock+mutate+save internally) — deliberately NOT a reuse of
// #DeployConfigSaveRequest (the `deploy-config-save` seam), which persists an ALREADY-LOADED,
// already-mutated whole BundleConfig with no internal load/lock/entry-removal logic (bundle
// import/reset's use case) — a genuinely different, narrower operation CleanDeployEntry's own
// internal file-lock + entry-removal + provides-cleanup + empty-file-delete logic cannot be
// reduced to.
#PodConfigCleanDeployEntryRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
}
#PodConfigCleanDeployEntryReply: {}

// #PodConfigHookSecretEnvRequest / Reply: resolveHookSecretEnv(box,instance,meta) — the
// credential-backed env the post_enable hook needs (same FINAL/K5-deferred family).
#PodConfigHookSecretEnvRequest: {
	box!:       string @go(Box)
	instance?:  string @go(Instance)
	meta_json!: bytes  @go(MetaJSON, type=RawBody)
}
#PodConfigHookSecretEnvReply: {
	env?: [...string] @go(Env)
}

// #PodConfigEncEnsurePlanRequest / Reply: the pod lifecycle's resolvePodEncEnsure body VERBATIM —
// encPlanFor + the keyring-resilient all-mounted fast path + resolveEncPassphrase, bundled into
// ONE narrow credential seam (the standing ruling) returning the pre-built spec.EncExecInput the
// plugin InvokeProviders verb:enc with directly (empty ⇒ no encrypted volumes configured or
// already-mounted fast path, matching the former ensureEncryptedMounts semantics).
#PodConfigEncEnsurePlanRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
}
#PodConfigEncEnsurePlanReply: {
	enc_json?: bytes @go(EncJSON, type=RawBody)
}

// #PodConfigEncUnmountPlanRequest / Reply: the pod lifecycle's resolvePodEncUnmount body —
// encPlanFor for the unmount leg (no passphrase needed).
#PodConfigEncUnmountPlanRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
}
#PodConfigEncUnmountPlanReply: {
	enc_json?: bytes @go(EncJSON, type=RawBody)
}

// #PodConfigContainerTunnelRequest / Reply: the pod lifecycle's resolvePodTunnel body — reads the
// RUNNING container's baked image ref (containerImage), extracts + merges its metadata, and
// resolves the tunnel config. Distinct from #PodConfigTunnelResolveRequest (which takes an
// already-resolved MetaJSON) — this seam resolves the image/metadata itself from a container name.
#PodConfigContainerTunnelRequest: {
	box!:      string @go(Box)
	instance?: string @go(Instance)
}
#PodConfigContainerTunnelReply: {
	tunnel_json?: bytes @go(TunnelJSON, type=RawBody)
}

// #PodConfigBoxEngineRequest / Reply: ResolveBoxEngineForDeploy(box,instance,globalEngine) — reads
// the per-host deploy config's Engine override. A thin wrapper distinct from
// #PodConfigLoadDeployRequest since callers here want only the resolved engine string, not the
// whole BundleConfig.
#PodConfigBoxEngineRequest: {
	box!:           string @go(Box)
	instance?:      string @go(Instance)
	global_engine!: string @go(GlobalEngine)
}
#PodConfigBoxEngineReply: {
	engine!: string @go(Engine)
}

// #PodConfigSSHKeyRequest / Reply: resolveSSHPubKey(flag, generateDir) + containerSSHKeyDir(name)
// bundle (the `--ssh-key generate` path is pure ed25519/golang.org/x/crypto/ssh keygen — kept as
// a narrow host seam rather than adding a crypto dependency to the plugin for a rarely-used flag).
#PodConfigSSHKeyRequest: {
	flag!:           string @go(Flag)
	container_name!: string @go(ContainerName)
}
#PodConfigSSHKeyReply: {
	pubkey?: string @go(Pubkey)
}

// #PodConfigListSidecarsReply: embeddedSidecarBodies()'s go:embed template names + descriptions —
// the `charly config --list-sidecars` introspection leaf (rare; kept as a narrow seam since the
// embedded data lives only in the charly binary).
#PodConfigListSidecarsReply: {
	names?: [...string] @go(Names)
	descriptions?: {[string]: string} @go(Descriptions)
}

// sdk.OpConfigSetup / sdk.OpConfigRemove (the two new Ops the deploy:pod plugin's Invoke
// dispatches for the direction-flip) reuse #PodConfigSetupRequest / #PodConfigRemoveRequest
// VERBATIM as op.Params — no new outer envelope needed; see host_build_pod_config.go for the
// exact host→plugin forwarding.

// #PodUpdateRequest carries the `charly update` command flags (the former UpdateCmd's
// authored fields). Forwarded to HostBuild("pod-update"), which runs the existing
// dispatchByDeployTarget orchestration VERBATIM — resolveTreeRoot/loadDeployPlugins/
// ResolveTarget are core Mechanisms (the project loader + provider registry) a plugin
// cannot import or hold.
#PodUpdateRequest: {
	box!:        string @go(Box)
	tag?:        string @go(Tag)
	build?:      bool   @go(Build)
	instance?:   string @go(Instance)
	seed?:       bool   @go(Seed)
	force_seed?: bool   @go(ForceSeed)
	data_from?:  string @go(DataFrom)
}

// #PodUpdateReply is the "pod-update" host-builder reply — empty, mirroring #PodStartReply.
#PodUpdateReply: {}

// #DeployTargetStatus (S3b, Unit-6 design) mirrors the former charly-core StatusInfo — a
// deployment's live runtime state, now CUE-sourced because it crosses the plugin boundary once
// externalDeployTarget/grpcSubstrateLifecycle move to candy/plugin-bundle.
#DeployTargetStatus: {
	state?:   string          @go(State)
	healthy?: bool            @go(Healthy)
	details?: {[string]: string} @go(Details)
}

// #DeployTargetDelOpts mirrors the former charly-core DelOpts (`charly bundle del`) PLUS the
// three teardown gates externalDeployTarget used to receive as externally-set STRUCT FIELDS
// (KeepRepoChanges/KeepServices/KeepImage, set via a type-assertion in
// host_build_deploy_node_del_dispatch.go before calling .Del()) — folded into DelOpts proper
// (S3b) since the moved target is now constructed fresh per call from data alone, with no
// settable-after-construction fields to type-assert onto.
#DeployTargetDelOpts: {
	dry_run?:             bool @go(DryRun)
	assume_yes?:          bool @go(AssumeYes)
	keep_ledger?:         bool @go(KeepLedger)
	remove_volumes?:      bool @go(RemoveVolumes)
	keep_repo_changes?:   bool @go(KeepRepoChanges)
	keep_services?:       bool @go(KeepServices)
	keep_image?:          bool @go(KeepImage)
}

// #DeployTargetTestOpts mirrors the former charly-core TestOpts (`charly check live`).
#DeployTargetTestOpts: {
	only_ids?:     [...string] @go(OnlyIDs)
	format_json?:  bool        @go(FormatJSON)
	stop_on_fail?: bool        @go(StopOnFail)
}

// (Removed, R10 bed-found bug fix, S3b): a prior discriminated Update-opts shape retired in
// favor of Update's OptsJSON marshaling the SAME #LifecycleOpts (CUE-sourced,
// schema/seam.cue) that Add's does — mirroring the pre-move Update path exactly, which
// built a plain deploykit.EmitOpts from the retired shape's fields and passed it into the SAME
// shared apply() body Add used, rather than a separate wire shape. RebuildImage is NEVER read by
// the apply body (it belongs to Rebuild's own #DeployTargetRebuildOpts) — the divergence the
// retired shape introduced silently dropped it before it could ever matter, but the REAL bug it
// masked was the Add path decoding a wire-incompatible raw EmitOpts (carrying the live
// ParentExec/ParentNode interface fields), which crashed the moment a nested-child deploy
// (ParentExec non-nil) tried to Add — proven on the check-sidecar-pod R10 bed. Full narrative:
// this repo's CHANGELOG/2026.203.0212.md.

// #DeployTargetLogsOpts mirrors the former charly-core LogsOpts (`charly logs`).
#DeployTargetLogsOpts: {
	follow?:  bool   @go(Follow)
	tail?:    int    @go(Tail)
	sidecar?: string @go(Sidecar)
}

// #DeployTargetRebuildOpts mirrors the former charly-core RebuildOpts (`charly update` rebuild path).
#DeployTargetRebuildOpts: {
	rebuild_image?: bool @go(RebuildImage)
	assume_yes?:    bool @go(AssumeYes)
	dry_run?:       bool @go(DryRun)
}

// #DeployTargetDispatchRequest (S3b) is the ONE generic host→command:bundle envelope every
// UnifiedDeployTarget/LifecycleTarget method dispatches through, discriminated by `op` (the
// project rulebook's "generic over ad-hoc" — one wire shape, not eleven). Core's thin
// ResolveTarget proxy (unified_targets.go) constructs this per call from data alone — it never
// holds a *grpcProvider or any core-private registry object, so the type is free to live
// entirely on the wire. `word` is the resolved deploy-substrate provider word (e.g. "pod"/"vm"/
// "local"/"k8s"/"android") the plugin dispatches the ACTUAL substrate leg to, via its own
// sdk.Executor.InvokeProvider — core never talks to the substrate provider directly once this
// lands. `has_lifecycle` is the ONE piece of substrate metadata core must resolve itself (the
// registered-provider's own `lifecycle` flag lives on the core-private *grpcProvider) — it gates
// whether Start/Stop/Status/Logs/Shell/Attach/Rebuild are even valid for this substrate
// (mirroring the former ErrNotSupportedOnExternal branches) AND whether the Q1 arbiter bracket
// applies to Start/Stop — `has_plan` is that DIFFERENT, narrower boolean (K4-exit, FLOOR-SLIM-
// proper Unit-8): core still computes it (lifecycleStartPlanHooks[word]/lifecycleStopPlanHooks[word]
// presence, pod_lifecycle_dispatch.go, unmoved) and now THREADS it on the wire instead of bracketing
// the dispatch call itself — command:bundle's handleLifecycleSimple owns the bracket call, via the
// arbiter-bracket-acquire/-release HostBuild seams below (the same os.Setenv/os.Getenv
// nested-subprocess-inheritance property the former arbiter_bracket.go preserved, now reached
// plugin-side). `node` is the dispatch-merged BundleNode
// (nil for a ref-based deploy with no charly.yml entry) — required when has_plan is true (the claim
// fields live on it). `plans_json` carries []InstallPlanView
// for Add/Update. `opts_json` carries the op-specific opts struct as an opaque envelope (the
// zero-value-safe pattern every #Pod*Opts request already uses) — kept opaque rather than one
// field per opts type so a NEW deploy verb never needs a NEW CUE field, only a new decode
// branch plugin-side.
#DeployTargetDispatchRequest: {
	op!:              "add" | "update" | "del" | "test" | "start" | "stop" | "status" | "logs" | "shell" | "attach" | "rebuild"
	name!:            string      @go(Name)
	word!:            string      @go(Word)
	has_lifecycle?:   bool        @go(HasLifecycle)
	has_preresolve?:  bool        @go(HasPreresolve)
	has_plan?:        bool        @go(HasPlan)
	node?:            #Deploy     @go(Node, type=*Deploy)
	dir?:             string      @go(Dir)
	node_only?:       bool        @go(NodeOnly)
	// ledger_root OPTIONALLY overrides the ledger root directory (kit.LedgerPaths.Root) — the
	// pre-S3b externalDeployTarget carried a settable `paths *kit.LedgerPaths` field a TEST could
	// redirect to a temp dir instead of the operator's real ~/.config/opencharly/installed/; this
	// is the wire-safe equivalent (a bare root string, the plugin derives Deploys/Candies/LockFile
	// from it exactly like kit.DefaultLedgerPaths does). Empty (the default) — kit.DefaultLedgerPaths().
	ledger_root?:     string      @go(LedgerRoot)
	plans_json?:      bytes       @go(PlansJSON, type=RawBody)
	opts_json?:       bytes       @go(OptsJSON, type=RawBody)
	checks_json?:     bytes       @go(ChecksJSON, type=RawBody)
	cmd?:             [...string] @go(Cmd)
	tty?:             bool        @go(TTY)
	// venue_json is the ALREADY-MATERIALIZED spec.VenueDescriptor for this deploy. Two distinct
	// producers set it: (a) core, when this dispatch is a NESTED non-lifecycle child (a
	// `local:`/`android:`/`k8s:` deploy under a vm/pod, tree position) — Add flattens the
	// live ancestor executor (EmitOpts.ParentExec) into this field via kit.DescriptorFromExecutor
	// BEFORE the very first "add" dispatch, since that live value cannot itself cross the wire
	// (FIX ROUND, S3b follow-up — its absence on "add" was the R10 bed regression: every nested
	// child silently applied on the operator's host instead of the parent venue); (b) core again,
	// carrying FORWARD a prior dispatch's reported venue for the SAME target's lifetime
	// (Update/Del/Start/Stop/Status/Logs/Shell/Attach/Rebuild, after a lifecycle substrate's "add"
	// already ran PrepareVenue once). Either way the plugin re-materializes it via
	// kit.VenueFromDescriptor instead of re-deriving from the node or re-running PrepareVenue.
	// Absent on "add" for a ROOT (non-nested) OR lifecycle (vm/pod) target — those derive their
	// own venue fresh (root: RootExecutorForDeployNode(node); lifecycle: PrepareVenue).
	venue_json?: bytes @go(VenueJSON, type=RawBody)
	// distro_cfg_json is the marshalled *buildkit.DistroConfig ("add"/"update" only) — recordDeploy's
	// FillReverseUninstallCmds needs it to render an aur-builder ReverseOpPackageRemove's
	// UninstallCmd host-side-equivalent, now plugin-side since buildkit.DistroConfig is a plain sdk
	// type with no core-only coupling (unlike the core-only buildEngineContext wrapper it used to
	// travel inside).
	distro_cfg_json?: bytes @go(DistroCfgJSON, type=RawBody)
	// host_env_json is the marshalled spec.HostEnv (CharlyBin/Home/Version) — the R10 bed-found
	// fifth bug in this cluster's move: every lifecycle Op the deleted substrate_lifecycle_grpc.go
	// sent used its OWN hostEnvJSON() helper, computed HOST-side (os.Executable() resolves to the
	// charly binary only when called IN CORE — a plugin's os.Executable() resolves to the PLUGIN
	// binary, wrong for an out-of-process placement even though today's compiled-in placement
	// happened not to crash on it, since a bare `spec.HostEnv{}` zero-value was marshalled instead
	// of ever actually calling it, plugin-side, in the S3b port). Core (unified_targets.go's
	// dispatch, the ONLY place that reliably knows its OWN binary regardless of the substrate's or
	// command:bundle's own placement) now computes it ONCE per dispatch call and threads it here;
	// candy/plugin-bundle forwards it verbatim to every lifecycle Op instead of computing its own.
	host_env_json?: bytes @go(HostEnvJSON, type=RawBody)
}

// #DeployTargetDispatchReply carries whatever the dispatched op produces — Status for "status",
// Output/ExitCode for "shell"/"attach" (the F12 non-zero-exit propagation via
// *sdk.ExitCodeError), Venue for "add"/"update" (the spec.VenueDescriptor PrepareVenue produced or
// re-confirmed, threaded back to core so a substrate WITH a lifecycle hook — pod's overlay
// container, vm's guest — hands core the SAME live executor for --verify and every subsequent
// dispatch on this target, mirroring the pre-S3b `t.exec = exec` reassignment inside apply()),
// nothing for the rest (success is "no error").
#DeployTargetDispatchReply: {
	status?:      #DeployTargetStatus @go(Status)
	output?:      string             @go(Output)
	exit_code?:   int                @go(ExitCode)
	venue_json?:  bytes              @go(VenueJSON, type=RawBody)
	artifact_key?: string            @go(ArtifactKey)
}

// ---------------------------------------------------------------------------
// Substrate LIFECYCLE wire (M4; SDD conversion of the former deploy_wire.go's
// lifecycle section, per the standing operator directive: a hand-written wire
// struct not yet CUE-sourced is conversion-in-progress, never a sanctioned
// exception) — the host↔plugin envelope for the pod/vm deploy lifecycle Ops.
// All ride Provider.Invoke params/env/reply JSON. Plain structs — gengotypes
// generates them faithfully, no disjunction needed.

// #LifecycleOpts is the serializable subset of the host's EmitOpts shipped in
// a lifecycle Op's params. The two LIVE EmitOpts fields (ParentExec,
// ParentNode) cannot cross the []byte wire — they re-attach host-side via the
// reverse channel's live host-build inputs, never serialized.
// LifecycleOptsFromEmit (spec/deploy_methods.go) is the ONE hand-written
// converter — a pure function, not a type, so it stays hand-written.
#LifecycleOpts: {
	dry_run?:                bool   @go(DryRun)
	allow_repo_changes?:     bool   @go(AllowRepoChanges)
	allow_root_tasks?:       bool   @go(AllowRootTasks)
	with_services?:          bool   @go(WithServices)
	assume_yes?:             bool   @go(AssumeYes)
	verify?:                 bool   @go(Verify)
	pull?:                   bool   @go(Pull)
	skip_incompatible?:      bool   @go(SkipIncompatible)
	builder_image_override?: string @go(BuilderImageOverride)
}

// #HostEnv is the generic host identity a lifecycle plugin (running ON the
// host) needs but cannot derive: the host charly binary path and the host
// home.
#HostEnv: {
	charly_bin?: string @go(CharlyBin)
	home?:       string @go(Home)
	// version is the host charly's CalVer (CharlyVersion()) — the
	// delivery-decision authority for EnsureCharlyInGuest.
	version?: string @go(Version)
}

// #LifecyclePrepareInput is the host-resolved DATA a vm substrate's
// OpPrepareVenue needs but cannot derive itself.
#LifecyclePrepareInput: {
	entity!: string @go(Entity) // the kind:vm ENTITY = disk/spec source (node.From-resolved)
	vm?:     #ResolvedVm @go(VM,optional=nillable) // the resolved vm value envelope (uf.VM[entity] via the plugin)
	ssh_user!:        string @go(SSHUser)        // resolveVmSshUser(spec)
	ssh_port!:        int    @go(SSHPort,type=int) // deploykit.ResolveVmSshPort(spec, domainIdentity) — per-deploy auto-alloc + persisted-port idempotency
	alias!:           string @go(Alias)          // VmSshAlias(domainIdentity) = charly-<deploy>
	ssh_key_path!:    string @go(SSHKeyPath)     // <stateDir>/id_ed25519
	known_hosts_path!: string @go(KnownHostsPath) // <stateDir>/known_hosts
	state_dir!:       string @go(StateDir)       // ~/.local/share/charly/vm/charly-<domainIdentity>
	prior_state?: #VmDeployState @go(PriorState,type=*VmDeployState) // the persisted VmDeployState (nil on first apply)
}

// #PrepareVenueReply is the OpPrepareVenue reply. Venue is re-materialized
// host-side into a live DeployExecutor (the live executor never crosses the
// wire); State is an opaque deploy-entry patch the host persists; Notes are
// human-facing lines the host prints.
#PrepareVenueReply: {
	venue!: #VenueDescriptor @go(Venue)
	state?: bytes @go(State,type=RawBody)
	notes?: [...string] @go(Notes)
}

// #PostTeardownReply is the OpPostTeardown reply: the host removes each named
// charly.yml deploy-entry key AFTER the plugin's teardown.
#PostTeardownReply: {
	remove_entries?: [...string] @go(RemoveEntries)
}

// #CliRequest is the "cli" host-builder envelope (M4): a lifecycle plugin
// asks the HOST to run a `charly <argv>` subcommand.
#CliRequest: {
	argv!: [...string] @go(Argv)
	capture?:     bool @go(Capture)
	combined?:    bool @go(Combined)
	best_effort?: bool @go(BestEffort)
}

// #CliReply is the "cli" host-builder reply: captured stdout (Capture=true),
// the exit code, and an error string on a non-zero exit that was not
// BestEffort-swallowed.
#CliReply: {
	stdout?:    string @go(Stdout)
	exit_code?: int    @go(ExitCode,type=int)
	error?:     string @go(Error)
}
