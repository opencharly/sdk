package loaderkit

import (
	"path/filepath"
	"slices"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// finalize_candy.go — the candy host-COMPLETION cluster (K3 U4-a), relocated verbatim from
// charly/layers.go so a plugin (candy/plugin-build) can complete + finalize a scanned candy set
// without the host. scanFromParsed (scan_candy.go) computes every term derivable from ONE candy
// alone; the two cross-cutting terms it defers — RunOps (needs the op-context classifier, task #39)
// and InitSystems (needs the project's init vocabulary) — are completed HERE. The op-context
// classifier is reached via the EXISTING spec.OpInContext DI hook (charly injects it at init:
// spec.OpInContext = opInContext, layers.go; candy/plugin-build injects an InvokeProvider-backed
// callback in U6) — so this cluster is now plugin-callable and #39's "the host still owns" deferral
// is CLOSED: loaderkit owns the completion, the op-context data rides the hook.

// PopulateCandyInitSystem sets the per-candy CandyView.InitSystems map based on the
// init config — the cross-candy host-completion pass (#67 pattern): scanning a
// SINGLE candy can't know the project's init vocabulary, so this runs once, after
// EVERY candy in the project has been scanned, over the mutable pre-wrap
// map[string]spec.ScannedCandy (a spec.CandyReader is read-only from here, so this
// MUST run before the final FinalizeCandyRefs+NewSpecCandyModel wrap — see
// ResolveOpts.InitCfg's doc comment). Byte-identical logic to the pre-move
// *Candy.InitSystems population, retargeted at scanned[name].Model.Service /
// .Model.SourceDir / .View.InitSystems.
// The per-candy DETECTION predicate itself lives in candyTriggersInit, below; this pass now
// only iterates the scanned set and records which inits each candy triggers.
func PopulateCandyInitSystem(scanned map[string]spec.ScannedCandy, initCfg *buildkit.InitConfig) {
	if initCfg == nil {
		return
	}
	for name, sc := range scanned {
		sc.View.InitSystems = make(map[string]bool)
		for initName, def := range initCfg.Init {
			if candyTriggersInit(sc, def) {
				sc.View.InitSystems[initName] = true
			}
		}
		scanned[name] = sc
	}
}

// candyTriggersInit reports whether the init definition def is triggered by the scanned candy
// sc. It is THE one detection predicate (R3): PopulateCandyInitSystem runs it over every candy
// of a project, and the init-runtime fetch seed (initDependsSeeds, init_depends_seed.go) asks the
// SAME question of the SAME pair — so the two can never disagree about whether a composition
// needs an init, which is what makes the seed complete rather than a guess.
//
// Schema-driven: a `service:` entry routes to the `use_packaged:` inits (ServiceSchema.
// SupportsPackaged) or to every init carrying a service_template. The `candy_file:` glob is an
// ORTHOGONAL arm covering the file_copy model — the `.service`/`.socket`/`.target`/`.timer`/
// `.path`/`.slice` units a candy SHIPS, a route a `service:` entry cannot express (a candy that
// ships unit files declares no `service:` list at all). Note the two arms are independent:
// supervisord sets supports_packaged: false, so a `use_packaged` entry triggers every init
// EXCEPT it, while an `exec:` entry triggers it alongside the others.
func candyTriggersInit(sc spec.ScannedCandy, def *spec.ResolvedInit) bool {
	if def == nil {
		return false
	}
	if slices.Contains(def.CandyFields, "service") {
		for i := range sc.Model.Service {
			entry := &sc.Model.Service[i]
			if entry.IsPackaged() {
				if def.ServiceSchema != nil && def.ServiceSchema.SupportsPackaged {
					return true
				}
				continue
			}
			if def.ServiceSchema != nil && def.ServiceSchema.ServiceTemplate != "" {
				return true
			}
		}
	}
	for _, pattern := range def.CandyFiles {
		matches, _ := filepath.Glob(filepath.Join(sc.Model.SourceDir, pattern))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

// CompleteCandyRunOps finishes the ONE host-completed predicate scanFromParsed's own doc comment
// flags as not scan-computable standalone: RunOps needs the op-context classifier (registry-adjacent
// D-data, reached via spec.OpInContext — task #39), so a single candy's scan can't derive it —
// this runs the SAME live-compute the pre-move *Candy.runOps() did (a `run:` step passes unless it is
// PURELY runtime-context), then OR-completes HasInstallFiles/HasContent with it (+ the already-known
// InitSystems term, when PopulateCandyInitSystem has run for this candy) — the associative-OR
// completion the scan-time partial computation deliberately deferred. MUST run on the mutable pre-wrap
// (Model, View) pair, before FinalizeCandyRefs+NewSpecCandyModel (a spec.CandyReader is read-only
// after that).
func CompleteCandyRunOps(m *spec.CandyModel, v *spec.CandyView) {
	for i := range m.Plan {
		step := &m.Plan[i]
		kw, err := step.StepKind()
		if err != nil || kw != kit.KwRun {
			continue
		}
		op := step.Op
		if spec.OpInContext(&op, spec.CtxRuntime) && !spec.OpInContext(&op, spec.CtxBuild) && !spec.OpInContext(&op, spec.CtxDeploy) {
			continue
		}
		m.RunOps = append(m.RunOps, op)
	}
	hasAnyInit := false
	for _, triggers := range v.InitSystems {
		if triggers {
			hasAnyInit = true
			break
		}
	}
	m.HasInstallFiles = m.HasInstallFiles || len(m.RunOps) > 0
	m.HasContent = m.HasContent || m.HasInstallFiles || hasAnyInit
}

// FinalizeScannedCandies is the SOLE choke point that produces a spec.CandyReader: every
// construction path (ScanCandy, legacyScanCandiesDirScanned via scanLocalCandies,
// (*loaderkit.spec.UnifiedFile).projectCandiesScanned via scanLocalCandies, and
// ScanAllCandyWithConfigOpts over its combined local+remote set) funnels through here, so no
// path can ever wrap a candy with a term (InitSystems, RunOps) still missing — there is no
// OTHER way to obtain a spec.CandyReader. Order: InitSystems (initCfg-gated; a nil initCfg is a
// documented no-op) THEN RunOps + the HasInstallFiles/HasContent OR-fold (unconditional) THEN
// FinalizeCandyRefs (bare-string the refs) THEN wrap — since a CandyReader is read-only from
// the wrap onward. Does NOT mutate its input map: PopulateCandyInitSystem mutates `scanned`
// in place when initCfg is non-nil, but every OTHER step below operates on a range-loop COPY,
// so calling this twice against the SAME map with different initCfg values (the throwaway
// nil-initCfg call ScanAllCandyWithConfigOpts makes for CollectRemoteRefsOpts's edge-walk,
// then the real opts.InitCfg call at the end) is safe.
func FinalizeScannedCandies(scanned map[string]spec.ScannedCandy, initCfg *buildkit.InitConfig) map[string]spec.CandyReader {
	PopulateCandyInitSystem(scanned, initCfg)
	out := make(map[string]spec.CandyReader, len(scanned))
	for name, sc := range scanned {
		CompleteCandyRunOps(&sc.Model, &sc.View)
		spec.FinalizeCandyRefs(&sc.Model, &sc.View, sc.Refs)
		out[name] = deploykit.NewSpecCandyModel(sc.Model, sc.View)
	}
	return out
}
