package deploykit

import (
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// resolved_project.go — the PURE box/candy PROJECTION helpers of the resolved-project envelope
// (K3 build-engine, Unit 2 body). Relocated from charly/resolved_project_host.go: each is PURE over
// (cfg, layers, resolved-box) — no registry, loader, filesystem, or package-main type — so both
// charly core AND candy/plugin-build (running the build-engine RESOLVE plugin-side) call the ONE
// copy (R3). The uf-COUPLED assembler + template projection stay in loaderkit (they read
// *spec.UnifiedFile); the host-coupled leg (resolveResources) stays host, while the namespaced-box
// resolve runs ENTIRELY plugin-side — see candy/plugin-build/resolve_legs.go's fillNamespacedBoxes +
// FillNamespaceBoxViews (spec_box_resolve.go). Both of its predecessors (the host namespaced-box fill
// and the `buildengine-namespaced` host leg) are deleted.

// ProjectResolvedBox projects a resolved box (buildkit.ResolvedBox) into the wire-safe
// spec.ResolvedBoxView: EXACTLY the non-json:"-" fields `charly box inspect` already serializes
// (json.MarshalIndent(*ResolvedBox)), in declaration order. The 6 json:"-" host-only compute caches
// are DROPPED — they are re-derivable by a resolving plugin (or reached via RunHostStep), never wire
// data (S-K5 verdict, the design key).
func ProjectResolvedBox(b *buildkit.ResolvedBox) spec.ResolvedBoxView {
	v := spec.ResolvedBoxView{
		Name:                  b.Name,
		Version:               b.Version,
		EffectiveVersion:      b.EffectiveVersion,
		Status:                b.Status,
		Info:                  b.Info,
		CheckLevel:            b.CheckLevel,
		Base:                  b.Base,
		From:                  b.From,
		BootstrapBuilderImage: b.BootstrapBuilderImage,
		Platforms:             b.Platforms,
		Tag:                   b.Tag,
		Registry:              b.Registry,
		Pkg:                   b.Pkg,
		Distro:                b.Distro,
		BuildFormats:          b.BuildFormats,
		Tags:                  b.Tags,
		Candy:                 b.Candy,
		User:                  b.User,
		UID:                   int64(b.UID),
		GID:                   int64(b.GID),
		Home:                  b.Home,
		UserAdopted:           b.UserAdopted,
		Merge:                 b.Merge,
		Builder:               map[string]string(b.Builder),
		BuilderCapabilities:   b.BuilderCapabilities,
		Auto:                  b.Auto,
		Network:               b.Network,
		DataImage:             b.DataImage,
		Entrypoint:            b.Entrypoint,
		Cmd:                   b.Cmd,
		IsExternalBase:        b.IsExternalBase,
		FullTag:               b.FullTag,
	}
	// build-RENDER caches (#67): copy when present. Filled ONLY in the build-render
	// projection (render-prep ran); empty for the validate/inspect path.
	v.BakedMetadata = b.BakedMetadata
	v.RenderCandyOrder = b.RenderCandyOrder
	v.InitSystem = b.InitSystem
	v.InitDef = b.InitDef
	v.ActiveInits = b.ActiveInits
	if b.CandyCaps != nil {
		v.Caps = &spec.AggregatedCandyCapsView{
			PreserveUser:       b.CandyCaps.PreserveUser,
			NeedsRootAfterInit: b.CandyCaps.NeedsRootAfterInit,
			OCILabels:          b.CandyCaps.OCILabels,
		}
	}
	return v
}

// RawCandyPair returns the underlying (spec.CandyModel, spec.CandyView) pair a wrapped
// spec.CandyReader carries — the W9 escape hatch (specCandyAdapter.RawCandy(), reached via a
// structural type assertion). ok is false only for a CandyReader implementer that isn't
// NewSpecCandyModel's adapter (no such implementer exists in production; a defensive,
// never-panicking fallback for the theoretical case).
func RawCandyPair(r spec.CandyReader) (spec.CandyModel, spec.CandyView, bool) {
	raw, ok := r.(interface {
		RawCandy() (spec.CandyModel, spec.CandyView)
	})
	if !ok {
		return spec.CandyModel{}, spec.CandyView{}, false
	}
	m, v := raw.RawCandy()
	return m, v, true
}

// ProjectBoxAggregates fills the box-AUTHORED + box-AGGREGATE fields on a ResolvedBoxView
// from the authored BoxConfig + the cross-candy collectors. The authored surfaces (Plan,
// AuthoredAliases) come from BoxOwner(cfg, name) — namespace-aware, since the build path keys
// preResolvedBoxes by the QUALIFIED name — and are SKIPPED for auto-intermediate boxes
// (which have no authored config). The aggregates (Ports/Volumes/Aliases/Engine) read
// cfg+layers by name and work for authored boxes AND intermediates — render-prep's
// buildBakedMetadata already used the same collectors for every gen.Box. A collector error
// leaves that aggregate empty (a read-only projection never fails the whole load). Shared by
// the pre-resolved (build-prep), fresh-resolve (validate), and auto-intermediate passes (R3).
func ProjectBoxAggregates(cfg *spec.Config, layers map[string]spec.CandyReader, name string, resolved *buildkit.ResolvedBox, view *spec.ResolvedBoxView) {
	if _, _, img, ok := BoxOwner(cfg, name); ok {
		view.Plan = img.Plan
		view.AuthoredAliases = img.Alias
		// K5-Unit-1 (#67 keystone): the box-AUTHORED deploy-overlay surfaces ExportAllBox reads
		// off the envelope instead of the live *Config graph. description is the RAW authored
		// string (Info above is its descriptionInfo first-line summary); env/env_file/security
		// are the box-authored deploy-overlay defaults. Filled alongside plan/authored_aliases.
		view.Description = img.Description
		view.Env = img.Env
		view.EnvFile = img.EnvFile
		view.Security = img.Security
	}
	if ports, perr := CollectBoxPorts(cfg, layers, name); perr == nil {
		view.Ports = ports
	}
	if vols, verr := CollectBoxVolume(cfg, layers, name, resolved.Home, nil); verr == nil {
		for _, vm := range vols {
			view.Volumes = append(view.Volumes, spec.ResolvedVolumeMount(vm))
		}
	}
	if als, aerr := CollectBoxAlias(cfg, layers, name); aerr == nil {
		for _, a := range als {
			view.Aliases = append(view.Aliases, spec.CandyAlias(a))
		}
	}
	view.Engine = ResolveBoxEngine(cfg, layers, name, "")
}

// FillBoxPlans populates out with the include-ready FLATTENED acceptance plan for every box
// reachable from cfg (its own boxes + every import namespace, recursively), keyed by QUALIFIED
// name (`fedora.jupyter`). It mirrors the former in-core `include: box:<name>` arm EXACTLY: the
// SAME CollectDescriptions base-chain walk (candy-chain bakeable steps + the box-level bakeable
// plan) flattened over the three sections, so the relocated plugin box arm reads a byte-equivalent
// plan without the resolve engine. Only boxes with a non-empty plan are recorded. The visited set
// guards the pointer-keyed namespace cache against a self-referential cycle.
func FillBoxPlans(cfg *spec.Config, layers map[string]spec.CandyReader, prefix string, out map[string][]spec.Step, visited map[*spec.Config]bool) {
	if cfg == nil || visited[cfg] {
		return
	}
	visited[cfg] = true
	for _, name := range cfg.AllBoxNames() {
		qualified := name
		if prefix != "" {
			qualified = prefix + "." + name
		}
		set := CollectDescriptions(cfg, layers, name)
		if set == nil {
			continue
		}
		var steps []spec.Step
		for _, sec := range [][]kit.LabeledDescription{set.Candy, set.Box, set.Deploy} {
			for _, ld := range sec {
				steps = append(steps, ld.Plan...)
			}
		}
		if len(steps) > 0 {
			out[qualified] = steps
		}
	}
	for ns, sub := range cfg.Namespaces {
		child := ns
		if prefix != "" {
			child = prefix + "." + ns
		}
		FillBoxPlans(sub, layers, child, out, visited)
	}
}
