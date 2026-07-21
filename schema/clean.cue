// CUE schema for the "retention" HostBuild kind wire types behind the externalized
// `charly clean` command plugin (candy/plugin-clean). NOT authoring kinds (never in
// #Node/#Op) — pure generated host<->plugin wire structs, single-sourced here so
// `task cue:gen` produces the Go structs (spec.RetentionRequest / spec.RetentionReply)
// package main's host_build_retention.go and candy/plugin-clean/command.go reference
// directly. Both are plain structs — gengotypes generates them faithfully, no
// disjunction/inexpressibility escape needed (per the SDD "wire types are CUE-sourced
// without exception" mandate).
//
// The command LOGIC (flag grammar, category orchestration, output) lives in the
// plugin; the SHARED retention engine (image-tag / build-candy / check-run / deep
// dangling-image pruning — also called by `charly box build`, `charly check run`,
// `charly box list tags`) stays in core and is reached via the generic "retention"
// HostBuild kind: the plugin asks the host to run the requested categories and gets
// back the removed refs/dirs/paths + the effective retention counts to present.

// #RetentionRequest is the "retention" HostBuild kind request: the plugin asks the
// host to run the shared prune engine host-side (the engine needs the core image
// inventory + label parsing that stays in core). dir is the project directory
// (os.Getwd() in the plugin) — always present, never empty. keep=0 means "use the
// resolved defaults"; invalidate (non-empty) runs ONLY the targeted image-tag
// invalidation. deep runs the store-wide untagged/dangling-image purge category
// (`charly clean --deep`) — unlike images (which only ever touches charly-labeled
// tags/dangling ids), deep removes EVERY untagged image in local storage, including
// unlabeled multi-stage build intermediates images can never see.
#RetentionRequest: {
	dir!:        string @go(Dir)
	dry_run?:    bool   @go(DryRun)
	images?:     bool   @go(Images)
	check?:      bool   @go(Check)
	deep?:       bool   @go(Deep)
	keep?:       int    @go(Keep, type=int)
	invalidate?: string @go(Invalidate)
}

// #RetentionReply is the "retention" HostBuild kind reply: the removed (or
// would-remove, under dry_run) image refs, build-candy dirs, and check-run paths,
// plus the effective retention counts, for the plugin to present.
//
// deep_ids/deep_bytes report the store-wide untagged-image purge (deep): the removed
// (or would-remove) image IDs and the sum of their reported storage Size in bytes.
// deep_bytes is an UPPER BOUND, not a prediction of actual freed disk: each image's
// reported Size counts every layer it references, and many of those layers are
// SHARED with images that remain (retained tags, or other still-referenced dangling
// images) — RDD-verified live on a real host, where a --deep purge removing 68
// untagged images (3,552 -> 3,484) reported ~92.6 GiB via this sum but only ~4.6 GiB
// of disk was actually freed (132.6 GB -> 128 GB), because most layer bytes stayed
// shared with the ~3,400 remaining (largely stale-tagged) images. `charly clean
// --deep --dry-run` presents deep_bytes as "up to" for exactly this reason; pairing
// --deep with --invalidate (removing stale TAGS too, so their exclusively-held
// layers also become unreferenced) gets closer to the reported figure.
//
// error is a human-facing message on a non-recoverable failure.
#RetentionReply: {
	image_refs?:      [...string] @go(ImageRefs)
	dangling_ids?:    [...string] @go(DanglingIDs)
	staging_dirs?:    [...string] @go(StagingDirs)
	build_dirs?:      [...string] @go(BuildDirs)
	check_paths?:     [...string] @go(CheckPaths)
	deep_ids?:        [...string] @go(DeepIDs)
	deep_bytes?:      int         @go(DeepBytes)
	keep_images?:     int         @go(KeepImages, type=int)
	keep_check_runs?: int         @go(KeepCheckRuns, type=int)
	error?:           string      @go(Error)
}
