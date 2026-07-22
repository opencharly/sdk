package spec

// arbiter_consts.go — the RESOURCE-ARBITER string-enum constants shared between charly's core and
// the COMPILED-IN candy/plugin-preempt (cutover C9; FLOOR-SLIM-proper Unit-8B SDD conversion).
// These are NOT wire-type structs — plain named string literals never carry a JSON/YAML shape for
// `cue exp gengotypes` to generate — so they stay hand-written here, mirroring the existing
// spec/gpu_consts.go precedent (a small hand-written consts file living beside its CUE-generated
// struct siblings, spec/cue_types_gen.go's #HolderAddr/#PreemptLease/#ArbiterInvokeInput/…). The
// struct types this file's consts tag (HolderAddr, PreemptedHolder, PreemptLease, PreemptLedger,
// HolderDescriptor, ArbiterGatherReply, ArbiterResourcesReply, ArbiterInvokeInput,
// ArbiterInvokeReply) are CUE-sourced at sdk/schema/arbiter.cue → generated into cue_types_gen.go.

// --- canonical preemption policy values (shared: the host Gather projection produces Restore,
// the plugin's releaseLeaseEffects reads it) ------------------------------------------------

const (
	// PreemptStopShutdown is the only supported stop mechanism (graceful ACPI shutdown / podman
	// stop; disk preserved).
	PreemptStopShutdown = "shutdown"
	// PreemptRestoreAlways restarts the holder regardless of the claim's outcome (the default).
	PreemptRestoreAlways = "always"
	// PreemptRestoreSuccess restarts the holder only if the claim released cleanly; on a failed
	// claim it is left stopped for operator inspection.
	PreemptRestoreSuccess = "on-success"
)

// --- HostArbiter reverse-channel seam action names (host serves, plugin calls mid-logic) -----
//
// One action-multiplexed RPC (ExecutorService.HostArbiter) carries the 2 seams that remain
// genuinely K1-blocked (project-config coupled via LoadUnified). FLOOR-SLIM-proper Unit-8 moved
// the other 6 (running/stop[+wait]/start/switchMode/ensureCDI/gpuCDI) directly into
// candy/plugin-preempt (holder_dispatch.go) — they were reached over this seam only because
// their ORIGINAL implementation used charly-core-private mechanisms, not because the work itself
// needed a live LoadUnified project; the plugin now dispatches the VM/GPU legs itself via the
// class-agnostic sdk.Executor.InvokeProvider. The plugin sends an ArbiterHost* request tagged by
// ArbiterAction*; the host runs the seam's CURRENT default implementation
// (gatherPreemptibleHolders/gatherResources) and replies.

const (
	ArbiterSeamGather    = "gather"    // -> ArbiterGatherReply (preemptible holders, projected)
	ArbiterSeamResources = "resources" // -> ArbiterResourcesReply (gpu-backed tokens -> vendor)
)

// --- verb:arbiter Invoke actions (the in-core PROXY -> the plugin) ---------------------------
//
// The in-core proxy resolves verb:arbiter and Invokes OpRun with an action-tagged
// ArbiterInvokeInput; the plugin runs the arbiter method and returns the matching reply. Mirrors
// gpu_shim's resolve+Invoke of verb:gpu.

const (
	ArbiterActionAcquireExclusive = "acquire-exclusive"
	ArbiterActionAcquireShared    = "acquire-shared"
	ArbiterActionRelease          = "release-claimant"
	ArbiterActionStatus           = "status"
	ArbiterActionReconcile        = "reconcile-stranded"
	ArbiterActionClearPoison      = "clear-poison"
	ArbiterActionResourcePoisoned = "resource-poisoned"
)
