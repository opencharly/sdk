// CUE schema for the BUILD-time host↔plugin seam wire types (P8b). The box-build
// engine's DRIVE — the podman build/push loop, per-image build lock, and the
// merge/retention orchestration — lives in candy/plugin-build (a compiled-in
// plugin importing ONLY sdk). It cannot LoadUnified, render Containerfiles, run
// the privileged builder-bootstrap, or ensure builder images — those stay core
// Mechanisms (the loader is P6's plugin + a kernel M/B; the runtime Candy graph
// is core by the P2 decision). The candy reaches them over the in-proc reverse
// channel: resolve+render → HostBuild("build-resolve"), layer merge →
// HostBuild("merge") (candy/plugin-oci's transitional seam, P14a). Each action
// noun is CLASS-GENERIC (never a provider word — the F11 uniform-API gate),
// mirroring the P10 config-resolve/vm-build seams in seam.cue.
//
// This REVERSES the P8 "permanent facade": P8 kept the whole engine host-side
// behind HostBuild("image"); P8b moves the DRIVE into the candy and leaves the
// host a pure RESOLVE/RENDER SEAM PROVIDER (build-resolve). generate.go /
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

// #BuildResolveBox is one image's drive descriptor: the tag to build, the
// rendered Containerfile CONTENT (piped to `podman build -f -` — shipped in the
// reply, NOT read from disk, to preserve the race-safety vs a concurrent
// generate overwrite), and whether merge.auto fires for it (the candy gates the
// HostBuild("merge") call on this bool; the host merge seam resolves the size
// knobs from config).
#BuildResolveBox: {
	name!:          string @go(Name)
	full_tag?:      string @go(FullTag)
	containerfile?: string @go(Containerfile)
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
// TRANSITIONAL — the shape below is T-P14a's finalized #MergeRequest/#MergeReply
// (sdk PR opencharly/sdk#28, schema/oci.cue). It is DUPLICATED here ONLY so P8b
// compiles + verifies BEFORE #28 merges into sdk main. When #28 lands, this sdk
// branch REBASES onto it, these two defs are DELETED from buildresolve.cue, and the
// consumers import spec.MergeRequest/MergeReply from oci.cue (same shape → zero code
// change) — team-lead's "one home = P14a's oci.cue" ruling. Do NOT add fields here;
// coordinate any change with T-P14a's oci.cue.
//
// The layer-merge seam: the candy's drive gates on a box's MergeAuto and, when set,
// asks the host to merge the just-built image by REF via HostBuild("merge") — the
// transitional seam that swaps to InvokeProvider("verb","oci",OpMerge,…) when P14a
// lands (and this seam + charly/mergeImageRef delete). ImageRef is the resolved
// <registry>/<name>:<tag> (P14a's pure engine cannot resolve box config → the candy
// passes the resolved ref + Engine + the per-box MaxMB/MaxTotalMB from the
// build-resolve model, 0 → host defaults). Class-generic action noun.
#MergeRequest: {
	image_ref!:    string @go(ImageRef)
	max_mb?:       int    @go(MaxMB)
	max_total_mb?: int    @go(MaxTotalMB)
	engine?:       string @go(Engine)
	dry_run?:      bool   @go(DryRun)
}

// #MergeReply (TRANSITIONAL, == P14a's) — LayersBefore/LayersAfter give the
// merged/kept summary (merged = before−after) for the drive log; Skipped when the
// image was too large; Error carries a per-merge FAILURE (reply-error convention;
// the image stays functional-but-unmerged, e.g. the known podman-load EEXIST case);
// Notes carry any diagnostic lines.
#MergeReply: {
	layers_before?: int    @go(LayersBefore)
	layers_after?:  int    @go(LayersAfter)
	skipped?:       bool   @go(Skipped)
	error?:         string @go(Error)
	notes?: [...string] @go(Notes)
}

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
	error?: string @go(Error)
}
