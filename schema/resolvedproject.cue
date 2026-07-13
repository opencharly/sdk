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

	// box-AGGREGATES — the cross-candy effective values `charly box inspect --format
	// ports|volumes|aliases|engine` prints (the host projector runs CollectBoxPorts /
	// CollectBoxVolume / CollectBoxAlias / ResolveBoxEngine into these). engine is the raw
	// resolver result ("" → the command body renders "(global default)").
	ports?: [...string] @go(Ports)
	volumes?: [...#ResolvedVolumeMount] @go(Volumes)
	aliases?: [...#CandyAlias] @go(Aliases)
	engine?: string @go(Engine)
}

// #ResolvedVolumeMount — one entry of the box-aggregate volume list (CollectBoxVolume): the
// charly-<box>-<name> volume name + the home-expanded container path. Mirrors deploykit.VolumeMount
// (a permanent deploykit home, never wire-marshaled there — spec carries the envelope copy).
#ResolvedVolumeMount: {
	volume_name?:    string @go(VolumeName)
	container_path?: string @go(ContainerPath)
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

	// per-candy list-subcommand sources: `charly box list routes|volumes|aliases|services`.
	// route/volumes/aliases carry the authored detail each subcommand prints (their presence IS
	// the RouteCandy/VolumeCandy/AliasCandy predicate). has_init + port_relay reconstruct the
	// InitCandy predicate (HasAnyInit() || PortRelayPorts>0) for `list services`.
	route?:    #RouteConfig @go(Route,optional=nillable)
	volumes?: [...#CandyVolume] @go(Volumes)
	aliases?: [...#CandyAlias] @go(Aliases)
	has_init?: bool @go(HasInit)
	port_relay?: [...int] @go(PortRelayPorts,type=[]int)
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
	// candy_models — the serializable candy BUILD models (validate, the plan-include splicer, K3-D)
	// keyed by name, distinct from candies (identity/graph). See candymodel.cue.
	candy_models?: {[string]: #CandyModel} @go(CandyModels)
	deploy?: {[string]: #Deploy} @go(Deploy,type=map[string]*Deploy)
	// build vocabulary (the validate ENGINE consumer): distro/init PIN the hand wire structs
	// (spec.ResolvedDistro/ResolvedInit — the distro/init de-type OpResolve envelopes, no #CUE def);
	// #Builder is a clean def. Pointer maps for parity with DistroConfig/BuilderConfig/InitConfig.Init.
	distro?:  {[string]: _} @go(Distro,type=map[string]*ResolvedDistro)
	builder?: {[string]: _} @go(Builder,type=map[string]*Builder)
	init?:    {[string]: _} @go(Init,type=map[string]*ResolvedInit)
	// kind templates (validate localtemplates + check-include pod/vm arms + status k8s/adb enumeration).
	templates?: #ProjectTemplates @go(Templates,optional=nillable)
	// kind:agent catalog (the harness AI-CLI pick — plugin-check reads it off this envelope; charly feature list-agent).
	agent_bodies?: {[string]: bytes} @go(AgentBodies,type=map[string]RawBody)
	// build order + auto-intermediates (charly box list targets).
	build_targets?: [...#BuildTarget] @go(BuildTargets)
	// box_plans — the include-ready FLATTENED acceptance plan per box (the `include: box:<name>`
	// arm of the plan-composition splicer). The host projector runs the SAME base-chain walk
	// CollectDescriptions uses (candy-chain bakeable steps + the box-level bakeable plan) and
	// flattens the three sections into one ordered []Step, keyed by the QUALIFIED box name
	// (namespaced boxes like `fedora.jupyter` included) — the exact result the former in-core
	// box arm produced. A plugin cannot recompute it (base-chain + candy-order + bakeable filter
	// are host resolve Mechanisms over the runtime Candy), so the resolve engine ships it. Only
	// boxes with a non-empty flattened plan appear.
	box_plans?: {[string]: [...#Step]} @go(BoxPlans)
}

// #ProjectTemplates — the bare pod:/vm:/local:/k8s:/android: template maps carried as OPAQUE payloads
// (the uf.Pod/VM/Local/K8s/Android raw bytes, verbatim). The host projector stays KIND-BLIND — it
// copies the raw template bytes with NO concrete-kind decode (a kernel that read spec.Local/#Pod/…
// would violate the boundary law + trip TestNoConcreteKindInKernel). The CONSUMING PLUGINS
// (validate localtemplates, check-include pod/vm arms, status k8s/adb) decode a RawBody into the
// concrete spec kind type themselves — a plugin MAY know kinds, the kernel may not.
#ProjectTemplates: {
	local?:   {[string]: bytes} @go(Local,type=map[string]RawBody)
	k8s?:     {[string]: bytes} @go(K8s,type=map[string]RawBody)
	pod?:     {[string]: bytes} @go(Pod,type=map[string]RawBody)
	vm?:      {[string]: bytes} @go(VM,type=map[string]RawBody)
	android?: {[string]: bytes} @go(Android,type=map[string]RawBody)
}

// #BuildTarget — a build-order entry (charly box list targets): the box/intermediate name + whether
// it is an auto-computed intermediate (the `name [auto]` output).
#BuildTarget: {
	name?: string @go(Name)
	auto?: bool   @go(Auto)
}

// #ResolvedProjectRequest — the `resolved-project` HostBuild request: which project dir to resolve
// (empty = the host's cwd) and whether to include enabled:false boxes. The reply is #ResolvedProject.
#ResolvedProjectRequest: {
	dir?:              string @go(Dir)
	include_disabled?: bool   @go(IncludeDisabled)
}
