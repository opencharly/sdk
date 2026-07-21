package spec

// clean_wire.go — wire types for the externalized `charly clean` command plugin (candy/plugin-clean).
// The command LOGIC (flag grammar, category orchestration, output) lives in the plugin; the SHARED
// retention engine (image-tag / build-candy / check-run pruning — also called by `charly box build`,
// `charly check run`, `charly box list tags`) stays in core and is reached via the generic "retention"
// HostBuild kind: the plugin asks the host to run the requested categories and gets back the removed
// refs/dirs/paths + the effective retention counts to present. The engine resolution (ResolveRuntime,
// keep_images/keep_check_runs defaults) happens host-side in the HostBuild handler.

// RetentionRequest is the "retention" HostBuild kind request: the plugin asks the host to run the
// shared prune engine host-side (the engine needs the core image inventory + label parsing that stays
// in core). Dir is the project directory (os.Getwd() in the plugin). Keep=0 means "use the resolved
// defaults"; Invalidate (non-empty) runs ONLY the targeted image-tag invalidation. Deep runs the
// store-wide untagged/dangling-image purge category (`charly clean --deep`) — unlike Images (which
// only ever touches charly-labeled tags/dangling ids), Deep removes EVERY untagged image in local
// storage, including unlabeled multi-stage build intermediates Images can never see.
type RetentionRequest struct {
	Dir        string `json:"dir"`
	DryRun     bool   `json:"dry_run,omitempty"`
	Images     bool   `json:"images,omitempty"`
	Check      bool   `json:"check,omitempty"`
	Deep       bool   `json:"deep,omitempty"`
	Keep       int    `json:"keep,omitempty"`
	Invalidate string `json:"invalidate,omitempty"`
}

// RetentionReply is the "retention" HostBuild kind reply: the removed (or would-remove, under DryRun)
// image refs, build-candy dirs, and check-run paths, plus the effective retention counts, for the
// plugin to present. DeepIDs/DeepBytes report the store-wide untagged-image purge (Deep): the removed
// (or would-remove) image IDs and the sum of their reported storage Size in bytes — the "reclaimable"
// figure `charly clean --deep --dry-run` prints. Error is a human-facing message on a non-recoverable
// failure.
type RetentionReply struct {
	ImageRefs     []string `json:"image_refs,omitempty"`
	DanglingIDs   []string `json:"dangling_ids,omitempty"`
	StagingDirs   []string `json:"staging_dirs,omitempty"`
	BuildDirs     []string `json:"build_dirs,omitempty"`
	CheckPaths    []string `json:"check_paths,omitempty"`
	DeepIDs       []string `json:"deep_ids,omitempty"`
	DeepBytes     int64    `json:"deep_bytes,omitempty"`
	KeepImages    int      `json:"keep_images,omitempty"`
	KeepCheckRuns int      `json:"keep_check_runs,omitempty"`
	Error         string   `json:"error,omitempty"`
}
