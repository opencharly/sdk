// CUE schema for the `validate-project` HostBuild seam (#46 OpValidateProject, task #60) — the
// generic project-validation channel the validate ENGINE (moving to plugin-box) consumes. The
// validate plugin runs the ~900 pure per-kind/op rules over the resolved-project envelope itself
// and re-runs the resolution-graph checks via sdk/deploykit (route-i); the HOST runs the
// checks a plugin structurally CANNOT (they need the RAW authored config, not a projection):
// CUE-schema conformance, the resource: vocab, and the build-tunable/merge rules (route-B). This
// seam returns those host-anchored findings PLUS the error-TOLERANT resolved projection (a box
// that fails to resolve becomes a diagnostic, not a fatal abort — validate MUST run on broken
// projects), so ONE round-trip gives the plugin both the partial envelope and the host diagnostics.
// Package-less; concatenated into the spec compilation unit. #Diagnostics is the shared wire type
// (deploy_wire.go), referenced by @go so the reply carries it without redefining it.

// #ValidateProjectRequest — which project dir to validate (empty = the host's cwd) + whether to
// include enabled:false boxes. Mirrors #ResolvedProjectRequest (the sibling resolved-project seam).
#ValidateProjectRequest: {
	dir?:              string @go(Dir)
	include_disabled?: bool   @go(IncludeDisabled)
}

// #ValidateProjectReply — the host-side project-validation result: the error-TOLERANT
// #ResolvedProject projection (partial; only boxes/candies that resolved) + the host-anchored
// diagnostics (per-box resolve failures + CUE-conformance + resource-vocab + build-tunable/merge).
// The plugin merges these with its own pure-rule + resolution-graph findings for the final verdict.
#ValidateProjectReply: {
	project?:     #ResolvedProject @go(Project,optional=nillable)
	diagnostics?: {...} @go(Diagnostics,type=Diagnostics)
}
