package deploykit

import (
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// spec_box_resolve.go — the buildkit→spec box-resolve BRIDGE for charly core (#55 Cluster-B,
// charly-0-buildkit). Core consumers of a resolved box read ONLY wire-clean spec.ResolvedBox
// fields (Name/Tags/Registry/Distro/…), never the buildkit host-render cache pointers, so they
// hold *spec.ResolvedBox. buildkit.ResolveBox / ResolveAllBox are PURE in-memory resolves —
// calling them HERE (deploykit imports buildkit, kit→kit) returns the full *buildkit.ResolvedBox,
// and we hand core the embedded wire-clean *spec.ResolvedBox. Deliberately NOT the
// resolved-project envelope InvokeProvider path: these are direct in-process calls, so there is NO
// process boundary and NO re-entrancy risk (an InvokeProvider("build",…) back into an in-flight
// plugin-build resolve callback would recurse — see #71 RE-ENTRANCY CAVEAT). deploykit is the ONE
// place that bridges buildkit's ResolvedBox to a spec consumer (it already owns NewSpecResolvedBox
// / ProjectResolvedBox); these siblings complete that bridge for the resolve direction.
//
// The opts carrier is deploykit-local + spec-typed (NOT loaderkit.ResolveOpts): loaderkit imports
// deploykit (loaderkit → deploykit), so deploykit importing loaderkit would be an import cycle.
// charly's resolveVocabOpts fills the fields from its own loaderkit.ResolveOpts before calling.

// SpecResolveOpts is the deploykit-local, spec-typed projection of the box-resolve options charly
// needs to pass in (a loaderkit-free carrier — see the file header for why loaderkit can't be
// named here). DistroCfg/BuilderCfg are the resolved build vocabulary (spec.DistroConfig /
// spec.BuilderConfig, alias-equal to buildkit's) the caller (charly's resolveVocabOpts) fills.
type SpecResolveOpts struct {
	IncludeDisabled      bool
	IncludeDisabledNames map[string]bool
	RequestedBoxes       []string
	DistroCfg            *spec.DistroConfig
	BuilderCfg           *spec.BuilderConfig
}

// shouldIncludeDisabled mirrors loaderkit.ResolveOpts.ShouldIncludeDisabled (IncludeDisabled OR a
// per-name override) — the enabled-gate relaxation for a specific box.
func (o SpecResolveOpts) shouldIncludeDisabled(name string) bool {
	return o.IncludeDisabled || o.IncludeDisabledNames[name]
}

// toBuildkit projects onto the buildkit.ResolveOpts the pure resolve consumes. Byte-equivalent to
// charly's former in-core build-vocab resolve-opts tail.
func (o SpecResolveOpts) toBuildkit() buildkit.ResolveOpts {
	return buildkit.ResolveOpts{
		IncludeDisabled:      o.IncludeDisabled,
		IncludeDisabledNames: o.IncludeDisabledNames,
		RequestedBoxes:       o.RequestedBoxes,
		DistroCfg:            o.DistroCfg,
		BuilderCfg:           o.BuilderCfg,
	}
}

// ResolveSpecBox resolves ONE box and returns its wire-clean *spec.ResolvedBox (the embedded value
// of the pure buildkit resolve). calver is the CalVer stamp (empty for a bare resolve).
func ResolveSpecBox(cfg *spec.Config, name, calver, dir string, opts SpecResolveOpts) (*spec.ResolvedBox, error) {
	resolved, err := buildkit.ResolveBox(cfg, name, calver, dir, opts.toBuildkit())
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, nil
	}
	return &resolved.ResolvedBox, nil
}

// ResolveAllSpecBoxes resolves every enabled box and returns their wire-clean *spec.ResolvedBox
// values — the render-seam-floor Generator's box set (it reads only Name/Tags off them).
func ResolveAllSpecBoxes(cfg *spec.Config, calver, dir string, opts SpecResolveOpts) (map[string]*spec.ResolvedBox, error) {
	images, err := buildkit.ResolveAllBox(cfg, calver, dir, opts.toBuildkit())
	if err != nil {
		return nil, err
	}
	out := make(map[string]*spec.ResolvedBox, len(images))
	for name, b := range images {
		if b == nil {
			continue
		}
		box := &b.ResolvedBox
		out[name] = box
	}
	return out, nil
}

// WrapSpecBoxes wraps wire-clean *spec.ResolvedBox values back into *buildkit.ResolvedBox for a
// deploykit render Generator. The host-render cache pointers are nil until a RenderPrep pass fills
// them (charly's toDeploykit uses this only to feed its now-spec-typed Generator.Boxes to the
// render engine; the render-seam floor + the box-build render-prep drive fill the caches).
func WrapSpecBoxes(m map[string]*spec.ResolvedBox) map[string]*buildkit.ResolvedBox {
	if m == nil {
		return nil
	}
	out := make(map[string]*buildkit.ResolvedBox, len(m))
	for name, b := range m {
		if b == nil {
			continue
		}
		out[name] = &buildkit.ResolvedBox{ResolvedBox: *b}
	}
	return out
}

// EffectiveBuilderNames returns the effective builder WORDS for a box (the buildkit resolve of the
// image→base→defaults builder chain, flattened via BuilderMap.AllBuilder). Pure over spec types;
// charly's refs.go remote-ref reachability walk calls it to add builder edges without naming
// buildkit.
func EffectiveBuilderNames(cfg *spec.Config, name string, img spec.BoxConfig) []string {
	return buildkit.EffectiveBuilderForBox(cfg, name, img).AllBuilder()
}

// FillNamespaceBoxViews resolves + render-preps every box in a namespace's own config and merges
// the resulting namespace-QUALIFIED spec.ResolvedBoxView into rp.Boxes (keyed child+"."+name),
// SKIPPING a box already present (a demand-pulled entry with correctly-requalified cross-refs —
// never overwrite it, per the RCA'd K1-alpha regression #2). It is the genuine build-engine
// render-prep DRIVE (buildkit.ResolveBox + RenderPrepBox + ProjectResolvedBox) the namespaced-box
// fill needs — relocated out of charly core (#55 Cluster-B) because it writes buildkit host-render
// caches. opts must already carry the project build vocabulary (charly's resolveVocabOpts fills it
// before calling). Byte-equivalent to the former in-core inner block of fillNamespacedBoxes.
func FillNamespaceBoxViews(sub *spec.Config, nsLayers map[string]spec.CandyReader, initCfg *spec.InitConfig, child, calver, dir string, opts SpecResolveOpts, rp *spec.ResolvedProject) {
	bkopts := opts.toBuildkit()
	subBoxes := map[string]*buildkit.ResolvedBox{}
	for _, name := range sub.AllBoxNames() {
		img, ok := sub.BoxConfig(name)
		if !ok || (!img.IsEnabled() && !opts.shouldIncludeDisabled(name)) {
			continue
		}
		resolved, err := buildkit.ResolveBox(sub, name, calver, dir, bkopts)
		if err != nil {
			continue
		}
		subBoxes[name] = resolved
	}
	if len(subBoxes) == 0 {
		return
	}
	tempGen := NewRenderGenerator()
	tempGen.Config = sub
	tempGen.Candies = nsLayers
	tempGen.InitConfig = initCfg
	tempGen.Dir = dir
	tempGen.Boxes = subBoxes
	for name, resolved := range subBoxes {
		fullKey := child + "." + name
		if rp.Boxes != nil {
			if _, exists := rp.Boxes[fullKey]; exists {
				continue
			}
		}
		// Best-effort: a namespaced box whose render-prep fails is projected WITHOUT the render
		// caches rather than dropped (it just can't serve as a builder/base stage) — matches the
		// former in-core tolerance.
		_ = tempGen.RenderPrepBox(name)
		view := ProjectResolvedBox(resolved)
		if rp.Boxes == nil {
			rp.Boxes = map[string]spec.ResolvedBoxView{}
		}
		rp.Boxes[fullKey] = view
	}
}
