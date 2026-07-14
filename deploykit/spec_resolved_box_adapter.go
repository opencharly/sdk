package deploykit

import (
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/spec"
)

// spec_resolved_box_adapter.go — the BOX analogue of specCandyAdapter (task #67, the
// build_resolve RENDER-leg death). A resolving PLUGIN (plugin-build) holds the
// resolved-project envelope — a spec.ResolvedBoxView per box + the project vocab maps
// (ResolvedProject.Distro / .Builder) — but the deploykit RENDER consumes a
// *buildkit.ResolvedBox with its json:"-" vocab pointers (DistroConfig/DistroDef/
// BuilderConfig) attached. This re-hydrator rebuilds that box from the envelope + the
// vocab, so a plugin renders Containerfiles WITHOUT the runtime *Candy/*ResolvedBox
// graph — it reads RESOLVED data (matching the specCandyAdapter grain, "no materialize
// seam"). The vocab types are ALIASES (buildkit.DistroDef = spec.ResolvedDistro,
// BuilderDef = spec.Builder), so the envelope maps wrap into the vocab config structs
// directly — no spec→buildkit conversion.

// NewSpecResolvedBox rebuilds a *buildkit.ResolvedBox from its resolved-project envelope
// view + the project vocab maps (ResolvedProject.Distro / .Builder). It mirrors the host
// projectResolvedBox projection IN REVERSE (scalars) and re-attaches exactly the vocab
// pointers the deploykit render reads: DistroConfig / DistroDef / BuilderConfig (~35
// img.DistroDef/DistroConfig/BuilderConfig read-sites). InitSystem/InitDef/CandyCaps are
// NOT re-attached — the render threads init via the activeInits param and reads no
// img.CandyCaps (build-mode caps are a host bootstrap concern, not a deploykit-render one).
func NewSpecResolvedBox(v spec.ResolvedBoxView, distro map[string]*spec.ResolvedDistro, builder map[string]*spec.Builder) *buildkit.ResolvedBox {
	box := &buildkit.ResolvedBox{
		Name:                  v.Name,
		Version:               v.Version,
		EffectiveVersion:      v.EffectiveVersion,
		Status:                v.Status,
		Info:                  v.Info,
		CheckLevel:            v.CheckLevel,
		Base:                  v.Base,
		From:                  v.From,
		BootstrapBuilderImage: v.BootstrapBuilderImage,
		Platforms:             v.Platforms,
		Tag:                   v.Tag,
		Registry:              v.Registry,
		Pkg:                   v.Pkg,
		Distro:                v.Distro,
		BuildFormats:          v.BuildFormats,
		Tags:                  v.Tags,
		Candy:                 v.Candy,
		User:                  v.User,
		UID:                   int(v.UID),
		GID:                   int(v.GID),
		Home:                  v.Home,
		UserAdopted:           v.UserAdopted,
		Merge:                 v.Merge,
		Builder:               buildkit.BuilderMap(v.Builder),
		BuilderCapabilities:   v.BuilderCapabilities,
		Auto:                  v.Auto,
		Network:               v.Network,
		DataImage:             v.DataImage,
		IsExternalBase:        v.IsExternalBase,
		FullTag:               v.FullTag,
	}
	// Re-attach the vocab the render reads (the json:"-" pointers). Vocab types are aliases,
	// so the envelope maps wrap directly into the config structs; ResolveDistro derives the
	// per-box DistroDef from the box's distro tags exactly as the host resolve path does.
	box.DistroConfig = &buildkit.DistroConfig{Distro: distro}
	box.DistroDef = box.DistroConfig.ResolveDistro(box.Distro)
	box.BuilderConfig = &buildkit.BuilderConfig{Builder: builder}
	// Re-attach the build-RENDER caches (#67 render-DRIVE move). Filled by the host
	// render-prep → projector; the deploykit render reads them WITHOUT the live graph.
	box.BakedMetadata = v.BakedMetadata
	box.RenderCandyOrder = v.RenderCandyOrder
	box.InitSystem = v.InitSystem
	box.InitDef = v.InitDef
	box.ActiveInits = v.ActiveInits
	if v.Caps != nil {
		box.CandyCaps = &buildkit.AggregatedCandyCaps{
			PreserveUser:       v.Caps.PreserveUser,
			NeedsRootAfterInit: v.Caps.NeedsRootAfterInit,
			OCILabels:          v.Caps.OCILabels,
		}
	}
	return box
}
