package loaderkit

import "github.com/opencharly/spec/spec"

// load_executor.go — the TYPED host-leg contract that makes loaderkit.LoadUnified runnable from a
// module that does NOT import charly core (the K1-LOADER RELOCATION goal). LoadUnified's kind-blind
// orchestration reaches every registry-/host-coupled step through a LoadSeams; LoadSeamsFromExecutor
// builds that LoadSeams from a LoaderExecutor — wiring the PURE, relocated LOAD-half seams DIRECTLY
// and dispatching the coupled steps through the executor. Two placements implement LoaderExecutor:
// the COMPILED-IN host (charly.LoadUnified) with its typed host funcs directly (zero marshal, U3),
// and every genuine PLUGIN (candy/plugin-fleet, candy/plugin-build, candy/plugin-vm — and any
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

// connectPassAwareExecutor is the OPTIONAL capability a LoaderExecutor may implement to report
// whether the current load is running inside the walk's connect-declared-kind pre-pass
// (inKindConnectPass). The compiled-in host implements it (charly's hostLoaderExecutor); an
// out-of-process plugin executor does not (plugins have no connect pass). LoadSeamsFromExecutor
// uses it to bypass the materialized-tree cache for the connect pass's transient, deferred-entity
// materialization (see the MaterializeLoadedProject seam below — R1, the external-kind decode
// regression). Structural: adding the method to a LoaderExecutor implementation needs NO spec
// change.
type connectPassAwareExecutor interface {
	InKindConnectPass() bool
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
		RunBootstrapPhase: exec.RunBootstrapPhase,
		WalkProject:       exec.WalkProject,
		// The materialize leg is the CUE-unify hotspot every CLI subcommand child re-runs (see
		// load_cache.go — the 32-lane stall root). Wrapping it with the host-side materialized-tree
		// cache here — the ONE seam constructor BOTH placements (compiled-in hostLoaderExecutor and
		// the out-of-process executorLoaderExecutor) drive LoadUnified through — lets same-state
		// loads across the wave's separate OS processes reuse the first child's materialization
		// instead of re-running the unify. The cache never fails a load: every cache failure
		// degrades to exec.MaterializeLoadedProject exactly as before.
		//
		// CONNECT-PASS BYPASS (R1, the external-kind decode regression): the walk's
		// connect-declared-kind pre-pass re-loads the project (connectDeclaredKindPlugins →
		// LoadConfig) with inKindConnectPass=true, and that nested load's materialize DEFERS the
		// declared-but-unconnected kind nodes (they decode only after the connect registers the
		// provider). Caching that deferred-entity tree under the SAME key the outer load then
		// reads would hand the outer load a tree with the kind entities MISSING — the observed
		// regression (charly's TestExternalKind_PrescanConnectDecode). The connect pass's
		// materialization is transient (it exists only to scan candies), so it must never be
		// cached: an executor that OPTIONALLY reports the connect-pass state (the compiled-in
		// host does; out-of-process plugins have no connect pass) bypasses the cache for the
		// duration of the pass, and the outer load re-materializes with the providers registered.
		MaterializeLoadedProject: func(lp *spec.LoadedProject, merged *spec.UnifiedFile, byID map[int64]*spec.UnifiedFile) error {
			if aware, ok := exec.(connectPassAwareExecutor); ok && aware.InKindConnectPass() {
				return exec.MaterializeLoadedProject(lp, merged, byID)
			}
			return MaterializeLoadedProjectCached(lp, merged, byID, exec.MaterializeLoadedProject)
		},
		FlattenFleetVenues:     FlattenFleetVenues,
		FoldMembers:            FoldMembers,
		StampFleetDescents:     func(uf *spec.UnifiedFile) { StampFleetDescents(uf, exec.LoaderThreaded()) },
		ValidateEphemeral:      func(uf *spec.UnifiedFile) error { return ValidateEphemeralUnified(uf, exec.LoaderThreaded()) },
		ValidateCheckBeds:      func(uf *spec.UnifiedFile) error { return ValidateCheckBeds(uf, exec.LoaderThreaded()) },
		ValidateAndroidDevices: exec.ValidateAndroidDevices,
		ValidateMembers:        ValidateMembers,
		ValidatePreemptible:    exec.ValidatePreemptible,
	}
}
