package loaderkit

import "github.com/opencharly/spec/spec"

// load_executor.go — the TYPED host-leg contract that makes loaderkit.LoadUnified runnable from a
// module that does NOT import charly core (the K1-LOADER RELOCATION goal). LoadUnified's kind-blind
// orchestration reaches every registry-/host-coupled step through a LoadSeams; LoadSeamsFromExecutor
// builds that LoadSeams from a LoaderExecutor — wiring the PURE, relocated LOAD-half seams DIRECTLY
// and dispatching the coupled steps through the executor. Two placements implement LoaderExecutor:
// the COMPILED-IN host (charly.LoadUnified) with its typed host funcs directly (zero marshal, U3),
// and every genuine PLUGIN (candy/plugin-bundle, candy/plugin-build, candy/plugin-vm — and any
// future reverse-channel consumer) over sdk.Executor's reverse channel, ALL through the ONE
// canonical plugin-side witness in this file (executorLoaderExecutor, load_via_executor.go) — no
// per-candy copies (K3-W2, task #13, hoisted the former private duplicates and deleted them, R3).
// This is the loader analogue of the DocParser / ProjectWalker / Materializer typed seams already in
// spec (loader_seam.go).

// LoaderExecutor is the typed host-leg contract for the registry-/host-coupled loader steps
// LoadUnified cannot do kind-blind. It is DEFINED in the dedicated spec module (spec.LoaderExecutor,
// #55 loader-keystone) so charly core can hold the host implementation while importing ONLY spec;
// this package-local ALIAS keeps loaderkit's own references (LoadSeamsFromExecutor,
// LoadUnifiedViaExecutor) terse. Two witnesses satisfy it structurally: charly's own
// hostLoaderExecutor (compiled-in, zero marshal) and this package's executorLoaderExecutor
// (load_via_executor.go — the ONE canonical plugin-side witness every reverse-channel consumer
// shares via LoadUnifiedViaExecutor).
type LoaderExecutor = spec.LoaderExecutor

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
		StampBundleDescents:      func(uf *spec.UnifiedFile) { StampBundleDescents(uf, exec.LoaderThreaded()) },
		ValidateEphemeral:        func(uf *spec.UnifiedFile) error { return ValidateEphemeralUnified(uf, exec.LoaderThreaded()) },
		ValidateCheckBeds:        func(uf *spec.UnifiedFile) error { return ValidateCheckBeds(uf, exec.LoaderThreaded()) },
		ValidateAndroidDevices:   exec.ValidateAndroidDevices,
		ValidateMembers:          ValidateMembers,
		ValidatePreemptible:      exec.ValidatePreemptible,
	}
}
