package loaderkit

import "github.com/opencharly/sdk/spec"

// load_executor.go — the TYPED host-leg contract that makes loaderkit.LoadUnified runnable from a
// module that does NOT import charly core (the K1-LOADER RELOCATION goal). LoadUnified's kind-blind
// orchestration reaches every registry-/host-coupled step through a LoadSeams; LoadSeamsFromExecutor
// builds that LoadSeams from a LoaderExecutor — wiring the PURE, relocated LOAD-half seams DIRECTLY
// and dispatching the coupled steps through the executor. Two placements implement LoaderExecutor:
// the COMPILED-IN host (charly.LoadUnified) with its typed host funcs directly (zero marshal, U3),
// and a genuine PLUGIN (the candy/plugin-bundle witness) over sdk.Executor's reverse channel. This
// is the loader analogue of the DocParser / ProjectWalker / Materializer typed seams already in
// sdk/spec (loader_seam.go).

// LoaderExecutor is the typed host-leg contract for the registry-/host-coupled loader steps
// LoadUnified cannot do kind-blind: the bootstrap-phase plugin invocation, the registry-coupled
// import/discover walk, the materialize kind-decode + merge, and the two registry-resolving
// validators. Because the methods are TYPED, a compiled-in placement pays no envelope tax; only a
// true out-of-module plugin marshals (the existing spec.LoadedProject / UnifiedFile envelopes).
type LoaderExecutor interface {
	// LoaderThreaded returns the CURRENT registry-derived snapshot (recognized kinds / deploy
	// substrates / DeployTraits / ExternalDeploySubstrates / …). Called FRESH at each DATA-seam
	// invocation — NEVER cached at seam-build time — because the walk's connect-declared-kind pass
	// mutates the registry BETWEEN seam construction and the post-walk validators, so a build-time
	// snapshot would be stale (matches charly's former per-seam loaderThreaded() calls exactly).
	LoaderThreaded() spec.Threaded
	// RunBootstrapPhase invokes every registered bootstrap-phase plugin on the raw root bytes,
	// returning the (possibly transformed) bytes.
	RunBootstrapPhase(data []byte) []byte
	// WalkProject runs the kind-blind import/discover/namespace walk (the registered
	// spec.ProjectWalker, reached via the host's spec.WalkSeams) → the generic spec.LoadedProject
	// envelope. The host #NodeDoc CUE gate (WalkSeams.GateDoc) runs INSIDE this walk.
	WalkProject(dir string, rootData []byte) (spec.LoadedProject, error)
	// MaterializeLoadedProject replays the host's per-document/per-namespace MATERIALIZE + root-wins
	// MERGE over the walk envelope (registry kind-decode via the registered spec.Materializer).
	// RULING 1: a TRANSITIONAL host leg — the whole embed+parser+registry orchestration stays
	// host-side until task #48 relocates it into loaderkit.
	MaterializeLoadedProject(lp *spec.LoadedProject, merged *UnifiedFile, byID map[int64]*UnifiedFile) error
	// ValidateAndroidDevices enforces the kind:android box⊻adb XOR — resolves android templates via
	// the provider registry (host-coupled), so a leg, not a pure loaderkit move.
	ValidateAndroidDevices(uf *UnifiedFile) error
	// ValidatePreemptible validates preemptible / requires_exclusive / requires_shared across the
	// deploy map, including the resource-vocabulary cross-check (resolves the resource plugin kind +
	// vm/resource entities via the registry) — host-coupled, so a leg.
	ValidatePreemptible(uf *UnifiedFile) error
}

// LoadSeamsFromExecutor builds a LoadSeams from a LoaderExecutor: the PURE, registry-free LOAD-half
// seams (relocated into loaderkit, K1-LOADER RELOCATION) are wired DIRECTLY; the registry-/host-
// coupled seams dispatch through exec. The DATA seams (descent stamp + ephemeral / check-bed
// validators) call exec.LoaderThreaded() FRESH inside their closures — matching charly's former
// per-seam loaderThreaded() calls, because the walk's connect pass mutates the registry mid-load so
// a build-time snapshot would be stale. This is the seam constructor a PLUGIN uses to run
// loaderkit.LoadUnified without importing charly core.
func LoadSeamsFromExecutor(exec LoaderExecutor) LoadSeams {
	return LoadSeams{
		RunBootstrapPhase:        exec.RunBootstrapPhase,
		WalkProject:              exec.WalkProject,
		MaterializeLoadedProject: exec.MaterializeLoadedProject,
		FlattenBundleVenues:      FlattenBundleVenues,
		FoldMembers:              FoldMembers,
		StampBundleDescents:      func(uf *UnifiedFile) { StampBundleDescents(uf, exec.LoaderThreaded()) },
		ValidateEphemeral:        func(uf *UnifiedFile) error { return ValidateEphemeralUnified(uf, exec.LoaderThreaded()) },
		ValidateCheckBeds:        func(uf *UnifiedFile) error { return ValidateCheckBeds(uf, exec.LoaderThreaded()) },
		ValidateAndroidDevices:   exec.ValidateAndroidDevices,
		ValidateMembers:          ValidateMembers,
		ValidatePreemptible:      exec.ValidatePreemptible,
	}
}
