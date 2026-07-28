package loaderkit

import (
	"fmt"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/spec"
)

// resolve_project.go — the RESOLVED-project ENVELOPE ASSEMBLER (K3 build-engine, Unit 2 body). It is
// the uf-COUPLED orchestration that walks the loaded project (*UnifiedFile: uf.Bundle, uf.Namespaces,
// uf.PluginKinds) and composes the generic spec.ResolvedProject envelope from the already-plugin-callable
// resolve primitives — deploykit.ProjectResolvedBox/ProjectBoxAggregates/RawCandyPair/FillBoxPlans/
// ResolveBoxOrder + loaderkit.ProjectTemplates. Its home is loaderkit (the package that owns UnifiedFile
// AND can import both deploykit+buildkit), so both charly core (a thin host wrapper) AND candy/plugin-build
// (running the build-engine RESOLVE plugin-side, U6) call the ONE copy (R3).
//
// The GENUINELY-host-coupled legs are INJECTED as ResolveProjectSeams (func fields), NOT baked in: the
// host wrapper builds closures capturing its ResolveOpts + registry; the plugin (U6) supplies
// InvokeProvider-backed / reverse-leg-backed closures. This keeps the assembler opts-agnostic — it never
// inspects a package-main ResolveOpts, only calls the seams — so the whole body compiles here with zero
// package-main dependency. `resolveResources` + `fillNamespacedBoxes` stay HOST for now (U5 turns them
// into a resource InvokeProvider leg + pre-computed inputs); `ResolveBox`/`ComputeIntermediates` are the
// pure buildkit/deploykit resolvers behind a thin host wrapper.

// ResolveProjectSeams carries the host-coupled legs the envelope assembler cannot run itself. Each is a
// closure the caller builds — the host over its in-proc ResolveOpts/registry, the plugin over its reverse
// channel. None inspects a concrete ResolveOpts here, so the assembler stays opts-agnostic.
type ResolveProjectSeams struct {
	// ResolveBox resolves ONE box. The host closure captures the ResolveOpts (incl. the pre-filled
	// DistroCfg/BuilderCfg that short-circuit fillBuildConfigFallback), so the assembler passes no opts.
	ResolveBox func(cfg *spec.Config, name, calver, dir string) (*buildkit.ResolvedBox, error)
	// FillNamespacedBoxes folds each import namespace's boxes (qualified) + their OWN candy sets into rp.
	// HOST (embeds a per-namespace scan + render-prep); becomes pre-computed inputs at U5.
	FillNamespacedBoxes func(uf *UnifiedFile, initCfg *buildkit.InitConfig, prefix, calver, dir string, rp *spec.ResolvedProject, visited map[*UnifiedFile]bool)
	// ResolveResources projects uf's `resource:` kind entities. HOST (per-node registry resolve);
	// becomes an InvokeProvider(ClassKind,"resource") leg at U5.
	ResolveResources func(uf *UnifiedFile) map[string]*spec.ResolvedResource
	// ShouldIncludeDisabled reports whether a disabled box's `enabled: false` gate is bypassed (host opts).
	ShouldIncludeDisabled func(name string) bool
	// ComputeIntermediates adds auto-generated intermediate images (host: lifts cfg.Defaults).
	ComputeIntermediates func(boxes map[string]*buildkit.ResolvedBox, layers map[string]spec.CandyReader, cfg *spec.Config, tag string) (map[string]*buildkit.ResolvedBox, error)
	// ExternalizedBuilders is the registry D-FACT (which builder words are served out-of-process). A fixed
	// compiled-in constant, threaded so the assembler stays free of the package-main var.
	ExternalizedBuilders map[string]bool
}

// ProjectResolvedProject assembles the spec.ResolvedProject from already-loaded resolve-engine outputs —
// a DATA projection over the seams, no resolution logic of its own. boxes come from seams.ResolveBox (or
// preResolvedBoxes), candies from the scanned layers map, deploy from the folded uf.Bundle tree, calver is
// the wall-clock build tag threaded by the caller. When diags is nil it is FAIL-FAST (a per-box ResolveBox
// failure aborts with an error). When diags is non-nil it is ERROR-TOLERANT (the validate-project path): a
// ResolveBox failure appends a spec.Diagnostic and SKIPS that box.
//
// preResolvedBoxes (the build-prep seam path) supplies boxes AS-IS — skipping the ResolveBox loop — so the
// render-prep caches (BakedMetadata/RenderCandyOrder/InitSystem/InitDef/ActiveInits/CandyCaps) are
// preserved on the ResolvedBoxView. nil (validate/inspect) resolves boxes fresh.
//
//nolint:gocyclo // envelope assembler — the box loop (pre-resolved vs fresh vs intermediate) + the candy/deploy/vocab projections; one branch per projection arm.
func ProjectResolvedProject(cfg *spec.Config, layers map[string]spec.CandyReader, uf *UnifiedFile, distroCfg *buildkit.DistroConfig, builderCfg *buildkit.BuilderConfig, initCfg *buildkit.InitConfig, dir, version, calver string, seams ResolveProjectSeams, diags *spec.Diagnostics, preResolvedBoxes map[string]*buildkit.ResolvedBox) (*spec.ResolvedProject, error) {
	rp := &spec.ResolvedProject{Version: version}

	resolvedBoxes := map[string]*buildkit.ResolvedBox{}
	for _, name := range cfg.AllBoxNames() {
		img, ok := cfg.BoxConfig(name)
		if !ok {
			continue
		}
		if !img.IsEnabled() && !seams.ShouldIncludeDisabled(name) {
			continue
		}
		// When pre-resolved boxes are provided (build-prep seam), use them directly —
		// render-prep has already filled the build-render caches on them.
		if preResolvedBoxes != nil {
			resolved, exists := preResolvedBoxes[name]
			if !exists {
				continue
			}
			resolvedBoxes[name] = resolved
			view := deploykit.ProjectResolvedBox(resolved)
			deploykit.ProjectBoxAggregates(cfg, layers, name, resolved, &view)
			if rp.Boxes == nil {
				rp.Boxes = make(map[string]spec.ResolvedBoxView, len(cfg.Box))
			}
			rp.Boxes[name] = view
			continue
		}
		resolved, err := seams.ResolveBox(cfg, name, calver, dir)
		if err != nil {
			if diags == nil {
				return nil, fmt.Errorf("resolving box %q: %w", name, err)
			}
			continue
		}
		resolvedBoxes[name] = resolved
		view := deploykit.ProjectResolvedBox(resolved)
		deploykit.ProjectBoxAggregates(cfg, layers, name, resolved, &view)
		if rp.Boxes == nil {
			rp.Boxes = make(map[string]spec.ResolvedBoxView, len(cfg.Box))
		}
		rp.Boxes[name] = view
	}

	// Auto-intermediates (#67): preResolvedBoxes (gen.Boxes) carries the auto-generated
	// intermediate images that cfg.allBoxNames() (authored-only) omits. The build order
	// returned to plugin-build includes them, so the render envelope must too — otherwise
	// dg.Generate(order) hits a box not in dg.Boxes and panics. The collectors read
	// cfg+layers by name and work for intermediates (render-prep's buildBakedMetadata
	// already used them for every gen.Box); an intermediate has no authored Plan/alias,
	// which deploykit.ProjectBoxAggregates skips via the cfg.BoxConfig(name) ok-check. A no-op range
	// when preResolvedBoxes is nil (the validate/inspect path passes nil).
	for name, resolved := range preResolvedBoxes {
		if _, exists := rp.Boxes[name]; exists {
			continue
		}
		resolvedBoxes[name] = resolved
		view := deploykit.ProjectResolvedBox(resolved)
		deploykit.ProjectBoxAggregates(cfg, layers, name, resolved, &view)
		if rp.Boxes == nil {
			rp.Boxes = make(map[string]spec.ResolvedBoxView, len(preResolvedBoxes))
		}
		rp.Boxes[name] = view
	}

	for name, c := range layers {
		if c == nil {
			continue
		}
		m, v, ok := deploykit.RawCandyPair(c)
		if !ok {
			continue
		}
		if rp.Candies == nil {
			rp.Candies = make(map[string]spec.CandyView, len(layers))
			rp.CandyModels = make(map[string]spec.CandyModel, len(layers))
		}
		rp.Candies[name] = v
		rp.CandyModels[name] = m
	}

	// namespace-qualified box views (`ns.name`) + each namespace's OWN candy scan folded into
	// rp.Candies/rp.CandyModels. HOST seam (embeds a per-namespace scan + render-prep). Runs AFTER the
	// root-scope candy fill above; best-effort/additive (a qualified key never collides with a bare name).
	if uf != nil {
		seams.FillNamespacedBoxes(uf, initCfg, "", calver, dir, rp, map[*UnifiedFile]bool{})
	}

	if uf != nil && len(uf.Bundle) > 0 {
		// BundleNode is a type alias for spec.Deploy, so the folded deploy tree projects into the
		// envelope's map[string]*Deploy directly (a per-iteration copy, addressed).
		rp.Deploy = make(map[string]*spec.Deploy, len(uf.Bundle))
		for k, v := range uf.Bundle {
			node := v
			rp.Deploy[k] = &node
		}
	}

	// resolved `resource:` kind entities. ResolvedResource is an intra-spec alias, so this is a straight
	// assignment. HOST seam (per-node registry resolve); becomes an InvokeProvider leg at U5.
	if uf != nil {
		if resources := seams.ResolveResources(uf); len(resources) > 0 {
			rp.Resources = resources
		}
	}

	// build VOCABULARY (the validate ENGINE consumer): the embedded distro/builder/init sections.
	if distroCfg != nil {
		rp.Distro = distroCfg.Distro
	}
	if builderCfg != nil {
		rp.Builder = builderCfg.Builder
	}
	if initCfg != nil {
		rp.Init = initCfg.Init
	}
	// ExternalizedBuilders (the registry D-FACT) — a fixed constant, threaded via the seams so a
	// resolved-project CONSUMER can dispatch a builder word without a separate host round-trip.
	rp.ExternalizedBuilders = seams.ExternalizedBuilders

	if uf != nil {
		// kind TEMPLATES (validate localtemplates + check-include pod/vm arms + status k8s/adb).
		rp.Templates = ProjectTemplates(uf)
		// kind:agent catalog (the harness AI-CLI pick + charly feature list-agent).
		if agents := uf.PluginKinds["agent"]; len(agents) > 0 {
			rp.AgentBodies = make(map[string]spec.RawBody, len(agents))
			for k, v := range agents {
				rp.AgentBodies[k] = v
			}
		}
	}

	// box_plans (the `include: box:<name>` plan-splice arm): the include-ready FLATTENED acceptance
	// plan per box, keyed by the QUALIFIED box name so a namespaced ref (fedora.jupyter) resolves.
	boxPlans := map[string][]spec.Step{}
	deploykit.FillBoxPlans(cfg, layers, "", boxPlans, map[*spec.Config]bool{})
	if len(boxPlans) > 0 {
		rp.BoxPlans = boxPlans
	}

	// build ORDER + auto-intermediates (charly box list targets): ComputeIntermediates adds the
	// auto-generated intermediate images; ResolveBoxOrder returns them dependency-ordered.
	if inter, ierr := seams.ComputeIntermediates(resolvedBoxes, layers, cfg, calver); ierr == nil {
		if order, oerr := deploykit.ResolveBoxOrder(inter, layers); oerr == nil {
			for _, name := range order {
				bt := spec.BuildTarget{Name: name}
				if b := inter[name]; b != nil {
					bt.Auto = b.Auto
				}
				rp.BuildTargets = append(rp.BuildTargets, bt)
			}
		}
	}

	return rp, nil
}
