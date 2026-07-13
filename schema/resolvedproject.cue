// CUE schema for the RESOLVED-project envelope (K5, the S-K5 keystone). #ResolvedProject is the
// generic, sdk-expressible RESOLVED/MATERIALIZED projection of a whole project — the third and
// final member of the envelope SPINE (spec.ParsedProject → #LoadedProject → #ResolvedProject).
// #LoadedProject is the PARSED, un-materialized stage; #ResolvedProject is what the host's resolve
// engines (ResolveBox / ScanAllCandy / the folded uf.Bundle deploy tree) already COMPUTE, serialized
// as ONE generic view the ~20k of K5 IOU consumers (inspect/list, the bundle-add box graph, status,
// the bed runner) read INSTEAD of the host types (*ResolvedBox / runtime Candy). It carries ONLY
// CONFIG+RESOLVE DATA: the host-only RESOLVE-time compute-cache pointers (DistroConfig / DistroDef /
// BuilderConfig / InitSystem / InitDef / CandyCaps — the 6 json:"-" fields of buildkit.ResolvedBox)
// and all LIVE state (container snapshots, engine truth, live executors) are EXCLUDED by design and
// stay host / verb-probe. Most of the envelope is ALREADY CUE-sourced (spec.Deploy, ParsedProject,
// DeploymentStatus, ResolvedK8s/Android); this file adds exactly TWO new views — #ResolvedBoxView +
// #CandyView — composed with the existing #Deploy tree.
//
// Package-less; concatenated into the spec compilation unit. NOT an authoring kind (never in
// #Node/#Op) — pure generated wire/projection types, single-sourced here so `task cue:gen` produces
// the Go structs (charly aliases them via vmshared/spec_aliases.go). Shared defs (#BoxMerge,
// #CandyRef, #CandyMCPProvide, #Deploy) come from box.cue / _common.cue / candy.cue / deploy.cue.

// #ResolvedBoxView — the resolved box METADATA a consumer reads: EXACTLY the non-json:"-" fields of
// buildkit.ResolvedBox, in declaration order, so this view is the wire-safe half of what
// `charly box inspect` already serializes (json.MarshalIndent(*ResolvedBox) — which never reaches the
// 6 json:"-" compute-cache pointers). Field order mirrors ResolvedBox so a future inspect relocation
// projects into this view byte-faithfully. Every field optional (a projection envelope); name is the
// stable key.
#ResolvedBoxView: {
	name!:                    string
	version?:                 string
	effective_version?:       string @go(EffectiveVersion)
	status?:                  string
	info?:                    string
	check_level?:             string @go(CheckLevel)
	base?:                    string
	from?:                    string
	bootstrap_builder_image?: string @go(BootstrapBuilderImage)
	platforms?: [...string]
	tag?:      string
	registry?: string
	pkg?:      string @go(Pkg)
	distro?: [...string]
	build_formats?: [...string] @go(BuildFormats)
	tags?: [...string]
	candy?: [...string]
	user?:         string
	uid?:          int  @go(UID)
	gid?:          int  @go(GID)
	home?:         string
	user_adopted?: bool      @go(UserAdopted)
	merge?:        #BoxMerge @go(Merge,optional=nillable)
	builder?: {[string]: string}
	builder_capabilities?: [...string] @go(BuilderCapabilities)
	auto?:             bool
	network?:          string
	data_image?:       bool @go(DataImage)
	is_external_base?: bool @go(IsExternalBase)
	full_tag?:         string @go(FullTag)
}

// #CandyView — the resolved candy GRAPH node a consumer reads: identity + dep-graph + provides +
// ports/services (the exported surface of the runtime Candy). The Has* filesystem probes, the
// unexported package/service sections, and the *CandyPluginDecl stay host — this is NOT the candy
// BUILD model (plan-steps / package-format sections), which is the CandyModel / S-CM concern (K3-D),
// distinct by design. require / candy pin to the bare map-key ref form (#CandyRef is @go(-), so a
// list of it generates []string). name is the stable key.
#CandyView: {
	name!:        string
	version?:     string
	description?: string
	status?:      string
	info?:        string
	remote?:      bool
	repo_path?:   string @go(RepoPath)
	is_plugin?:   bool   @go(IsPlugin)
	require?: [...#CandyRef] @go(Require)
	candy?: [...#CandyRef] @go(IncludedCandy)
	env_provide?: {[string]: string} @go(EnvProvides)
	mcp_provide?: [...#CandyMCPProvide] @go(MCPProvide)
	port?: [...int] @go(Ports)
	service_name?: [...string] @go(ServiceNames)
}

// #ResolvedProject — the whole resolved projection: the schema version, the resolved boxes keyed by
// name, the resolved candy graph keyed by name, and the deploy tree (uf.Bundle verbatim — already
// map[string]spec.Deploy). The deploy map is @go-pinned to a pointer map so `cue exp gengotypes`
// generates map[string]*Deploy (recursive tree, faithful). provides/sidecar are additive later
// members of this same envelope (added by the consumer unit that first needs them).
#ResolvedProject: {
	version?: string
	boxes?: {[string]: #ResolvedBoxView}
	candies?: {[string]: #CandyView}
	deploy?: {[string]: #Deploy} @go(Deploy,type=map[string]*Deploy)
}

// #ResolvedProjectRequest — the `resolved-project` HostBuild request: which project dir to resolve
// (empty = the host's cwd) and whether to include enabled:false boxes. The reply is #ResolvedProject.
#ResolvedProjectRequest: {
	dir?:              string @go(Dir)
	include_disabled?: bool   @go(IncludeDisabled)
}
