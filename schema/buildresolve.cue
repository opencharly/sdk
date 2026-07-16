// CUE schema for the BUILD-time host↔plugin seam wire types (P8b). The box-build
// engine's DRIVE — the podman build/push loop, per-image build lock, and the
// merge/retention orchestration — lives in candy/plugin-build (a compiled-in
// plugin importing ONLY sdk). It cannot LoadUnified, render Containerfiles, run
// the privileged builder-bootstrap, or ensure builder images — those stay core
// Mechanisms (the loader is P6's plugin + a kernel M/B; the runtime Candy graph
// is core by the P2 decision). The candy reaches them over the in-proc reverse
// channel: resolve+render → HostBuild("build-prep"), layer merge →
// HostBuild("merge") (candy/plugin-oci's transitional seam, P14a). Each action
// noun is CLASS-GENERIC (never a provider word — the F11 uniform-API gate),
// mirroring the P10 config-resolve/vm-build seams in seam.cue.
//
// This REVERSES the P8 "permanent facade": P8 kept the whole engine host-side
// behind HostBuild("image"); P8b moves the DRIVE into the candy and leaves the
// host a pure RESOLVE/RENDER SEAM PROVIDER (build-prep). generate.go /
// OCITarget / intermediates / layers stay core PINNED BY the loader Mechanism +
// the P2 runtime-Candy decision — re-judged at P15/P16 after the loader fold.
//
// Package-less; concatenated into the spec compilation unit. NOT authoring kinds
// (never in #Node/#Op) — pure generated wire types, single-sourced here so
// `task cue:gen` produces the Go structs (WIRE TYPES ARE CUE-SOURCED WITHOUT
// EXCEPTION, CLAUDE.md SDD). All fields are plain scalars/lists/nested structs
// that `cue exp gengotypes` generates faithfully — no disjunction, no RawBody
// envelope needed (the per-box descriptor carries only the drive-relevant
// scalars, NOT the full hand-written ResolvedBox).

// #BuildResolveRequest carries the CLI-supplied build inputs (the former
// BuildRequest fields) the host cannot reconstruct from Dir alone. The host runs
// NewGenerator (loader) + Generate (render → .build/), the privileged
// builder-bootstrap, and the builder-image ensure host-side, then returns the
// drive-model. GenerateOnly (the `charly box generate` path) renders + returns
// the written Containerfile paths WITHOUT bootstrap/ensure/build-prep.
#BuildResolveRequest: {
	boxes?: [...string] @go(Boxes)
	tag?:              string @go(Tag)
	dir?:              string @go(Dir)
	include_disabled?: bool   @go(IncludeDisabled)
	dev_local_pkg?:    bool   @go(DevLocalPkg)
	push?:             bool   @go(Push)
	platform?:         string @go(Platform)
	cache?:            string @go(Cache)
	no_cache?:         bool   @go(NoCache)
	jobs?:             int    @go(Jobs)
	podman_jobs?:      int    @go(PodmanJobs)
	generate_only?:    bool   @go(GenerateOnly)
}

// #BuildResolveBox is one image's drive descriptor: the tag to build and whether
// merge.auto fires for it (the candy gates the HostBuild("merge") call on this
// bool; the host merge seam resolves the size knobs from config). The rendered
// Containerfile CONTENT is NO LONGER shipped in the reply (#67 render-DRIVE
// move): plugin-build renders Containerfiles itself from the resolved-project
// envelope (BuildResolveReply.resolved_project) via deploykit.Generator, so the
// candy pipes dg.Containerfiles[name] to podman — not a reply field.
#BuildResolveBox: {
	name!:          string @go(Name)
	full_tag?:      string @go(FullTag)
	registry?:      string @go(Registry)
	platforms?: [...string] @go(Platforms)
	merge_auto?:         bool @go(MergeAuto)
	merge_max_mb?:       int  @go(MergeMaxMB)
	merge_max_total_mb?: int  @go(MergeMaxTotalMB)
}

// #BuildResolveReply is the host-resolved drive-model the candy's podman drive
// consumes. Order is the filtered SEQUENTIAL build order (non-empty for a
// `charly box build <name…>` selection); Levels is the level-PARALLEL order (the
// full build) — exactly one is set, mirroring buildImages' two branches. Boxes
// carries the per-image descriptors keyed by Name. Jobs/PodmanJobs/Cache are the
// resolved build tunables (from Config.Defaults + flags/env). KeepImages rides
// back for the host-side retention post-step. Written is the generate-only
// result (the emitted Containerfile paths). Error carries a build FAILURE (the
// reply-error convention; the RPC itself succeeds).
// The layer-merge seam types (#MergeRequest / #MergeReply) live in schema/oci.cue
// (owned by P14a's OCI cutover) — the ONE home (team-lead ruling). The candy's
// HostBuild("merge") seam + charly/mergeImageRef import spec.MergeRequest/MergeReply
// from there; this file defines only the BuildResolve* family.

#BuildResolveReply: {
	engine?:      string @go(Engine)
	engine_name?: string @go(EngineName)
	platform?:    string @go(Platform)
	order?: [...string] @go(Order)
	levels?: [...[...string]] @go(Levels)
	boxes?: [...#BuildResolveBox] @go(Boxes)
	jobs?:        int @go(Jobs)
	podman_jobs?: int @go(PodmanJobs)
	cache?:       string @go(Cache)
	keep_images?: int @go(KeepImages)
	written?: [...string] @go(Written)
	// resolved_project — the full resolved-project envelope with build-render
	// caches (BakedMetadata/RenderCandyOrder/InitSystem/InitDef/ActiveInits/Caps
	// on each ResolvedBoxView + GlobalOrder/ExternalizedBuilders on the project),
	// so plugin-build renders Containerfiles via deploykit.Generator WITHOUT the
	// live *Candy/*Config graph (#67 render-DRIVE move). Filled by the build-prep
	// seam (render-prep → projectResolvedProject with caches). Empty for the
	// generate-only path that writes Containerfiles host-side (transitional).
	resolved_project?: #ResolvedProject @go(ResolvedProject,optional=nillable)
	error?: string @go(Error)
}

// #BakePluginsRequest carries the inputs the host-side bake-plugins seam needs: the
// project dir (to load the live *Candy graph for SourceDir + buildPluginBinary), the box
// name (for the staging dir), and the candy order (the composition being baked). The host
// builds + stages each bake_plugin binary + returns the COPY/chmod fragment (#67).
#BakePluginsRequest: {
	dir?:        string @go(Dir)
	box_name!:   string @go(BoxName)
	candy_order?: [...string] @go(CandyOrder)
}

// #BakePluginsReply carries the rendered Containerfile fragment (COPY + chmod lines for
// each bake_plugin binary) + the reply-error convention.
#BakePluginsReply: {
	fragment?: string @go(Fragment)
	error?:    string @go(Error)
}

// #RenderSeamRequest is the generic host↔plugin render-seam dispatch (#67 render-DRIVE move).
// plugin-build wires the deploykit.Generator seams via HostBuild("render-seam") with a method
// discriminator + opaque JSON params. The host dispatches by method to the corresponding core
// function. This is the SINGLE HostBuild kind for ALL render seams that need host callbacks
// (RenderService, ValidateEgress, EmitPluginOp, etc.) — one CUE type, many methods.
#RenderSeamRequest: {
	method!: string @go(Method)
	params?: bytes @go(Params)
}

// #RenderSeamReply carries the opaque JSON result + the reply-error convention.
#RenderSeamReply: {
	result?: bytes @go(Result)
	error?:  string @go(Error)
}
