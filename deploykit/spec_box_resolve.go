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
// The box-resolve options are the SHARED spec.ResolveOpts (the loader scan/load options, relocated
// to the dedicated spec module in the #55 loader cascade): deploykit imports spec, so these entry
// points consume spec.ResolveOpts DIRECTLY — the former deploykit-local SpecResolveOpts twin (kept
// only because the loader options once lived in loaderkit, which deploykit can't import) is dissolved
// (#55 2b R3). charly's resolveVocabOpts fills the build vocabulary (DistroCfg/BuilderCfg) on the
// spec.ResolveOpts before calling; only the box-resolve subset (the 5 fields below) is read here —
// ExtraCandyRefs/InitCfg are consumed by the candy scan, never the box resolve.

// specToBuildkit projects the box-resolve subset of a spec.ResolveOpts onto the buildkit.ResolveOpts
// the pure resolve consumes. Byte-equivalent to the former SpecResolveOpts.toBuildkit tail.
func specToBuildkit(o spec.ResolveOpts) buildkit.ResolveOpts {
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
func ResolveSpecBox(cfg *spec.Config, name, calver, dir string, opts spec.ResolveOpts) (*spec.ResolvedBox, error) {
	resolved, err := buildkit.ResolveBox(cfg, name, calver, dir, specToBuildkit(opts))
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
func ResolveAllSpecBoxes(cfg *spec.Config, calver, dir string, opts spec.ResolveOpts) (map[string]*spec.ResolvedBox, error) {
	images, err := buildkit.ResolveAllBox(cfg, calver, dir, specToBuildkit(opts))
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

// SpecBoxes is the forward inverse of WrapSpecBoxes: it projects an already-resolved
// buildkit box map to the wire-clean *spec.ResolvedBox map (each entry's embedded spec value, by
// address) — the SAME conversion ResolveAllSpecBoxes performs inline, factored out so a caller
// that already ran buildkit.ResolveAllBox (candy/plugin-build's resolveBuildEngine) can push its
// resolved boxes to the host's buildengine-prep leg via #ResolvedProjectRequest.boxes WITHOUT the
// host re-resolving. #55 coneB2 Class B — sheds the deploykit import from charly/generate.go.
func SpecBoxes(m map[string]*buildkit.ResolvedBox) map[string]*spec.ResolvedBox {
	if m == nil {
		return nil
	}
	out := make(map[string]*spec.ResolvedBox, len(m))
	for name, b := range m {
		if b == nil {
			continue
		}
		out[name] = &b.ResolvedBox
	}
	return out
}

// FillNamespaceBoxViews resolves + render-preps every box in a namespace's own config and merges
// the resulting namespace-QUALIFIED spec.ResolvedBoxView into rp.Boxes (keyed child+"."+name),
// SKIPPING a box already present (a demand-pulled entry with correctly-requalified cross-refs —
// never overwrite it, per the RCA'd K1-alpha regression #2). It is the genuine build-engine
// render-prep DRIVE (buildkit.ResolveBox + RenderPrepBox + ProjectResolvedBox) the namespaced-box
// fill needs — relocated out of charly core (#55 Cluster-B) because it writes buildkit host-render
// caches. opts must already carry the project build vocabulary (charly's resolveVocabOpts fills it
// before calling). Byte-equivalent to the former in-core inner block of the deleted host namespaced-box fill.
func FillNamespaceBoxViews(sub *spec.Config, nsLayers map[string]spec.CandyReader, initCfg *spec.InitConfig, child, calver, dir string, opts spec.ResolveOpts, rp *spec.ResolvedProject) {
	bkopts := specToBuildkit(opts)
	subBoxes := map[string]*buildkit.ResolvedBox{}
	for _, name := range sub.AllBoxNames() {
		img, ok := sub.BoxConfig(name)
		if !ok || (!img.IsEnabled() && !opts.ShouldIncludeDisabled(name)) {
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
	// The init `depends_candy:` injection, for the NAMESPACED box set. This is the THIRD box-
	// composition path (beside candy/plugin-build's resolveBuildEngine and
	// loaderkit.ProjectResolvedProject's fresh-box loop) and it needs the pass just as much: an
	// import namespace's boxes are resolved from the namespace's OWN config here, so they never
	// pass through either of the other two seams. Without it a namespaced box composing service
	// candies would get ai.opencharly.init baked by the RenderPrepBox below — the init RESOLVED —
	// while its init candy was never added, which is exactly the defect this pass exists to close.
	// MUST run before the render-prep loop so RenderCandyOrder/BakedMetadata see the injected candy.
	InjectInitDependsCandy(subBoxes, nsLayers, initCfg)
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
