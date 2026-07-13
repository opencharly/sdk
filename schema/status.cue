// CUE schema for the deployment-status wire types — the shared shape the
// command:status plugin renders and every substrate plugin's status-collect Op
// returns. Package-less; concatenated into the spec compilation unit. These are
// NOT authoring kinds (never in #Node/#Op) — pure generated wire/render structs,
// single-sourced here so `task cue:gen` produces the Go structs (charly aliases
// them via vmshared/spec_aliases.go). @go names match the Go field names the
// renderers reference; JSON tags drive both the `charly status --json` output and
// the host<->plugin wire.

// #PortMapping — one published port's structured runtime mapping (host IP/port ->
// container port/proto). Surfaces on #DeploymentStatus so renderers + host probes
// consume it without re-parsing.
#PortMapping: {
	host_ip?:        string @go(HostIP)
	host_port:       int    @go(HostPort,type=int)
	container_port:  int    @go(CtrPort,type=int)
	protocol:        string @go(Proto)
}

// #ToolStatus — one live tool-probe result row. status "-" means the tool is not
// configured in this deployment; it is filtered before render.
#ToolStatus: {
	name:    string @go(Name)
	status:  string @go(Status)
	port?:   int    @go(Port,type=int)
	detail?: string @go(Detail)
}

// #SubstrateKind — the substrate discriminator enum. CUE validates the value at
// the wire boundary; @go(-) suppresses a (degraded) generated Go type — the named
// Go type + typed consts are hand-written in spec/status_types.go and referenced by
// #DeploymentStatus.kind via @go(Kind,type=SubstrateKind).
#SubstrateKind: "pod" | "vm" | "k8s" | "local" | "android" @go(-)

// #DeploymentStatus — the rendered shape for the table + JSON outputs across every
// deployment substrate (pod / vm / k8s / local / android). kind discriminates the
// substrate; nested carries multi-hop children (RECURSIVE self-reference, populated
// by the nested overlay); source records provenance (libvirt|ledger|adb|tree|podman).
#DeploymentStatus: {
	kind:       #SubstrateKind @go(Kind,type=SubstrateKind)
	image:      string @go(Image)
	image_ref?: string @go(ImageRef)
	instance?:  string @go(Instance)
	status:     string @go(Status)
	uptime?:    string @go(Uptime)
	container:  string @go(Container)
	ports?: [...#PortMapping] @go(Ports)
	devices?: [...string] @go(Devices)
	tools?: [...#ToolStatus] @go(Tools)
	volumes?: [...string] @go(Volumes)
	network?:  string @go(Network)
	tunnel?:   string @go(Tunnel)
	secrets?: [...string] @go(Secrets)
	run_mode: string @go(RunMode)
	nested?: [...#DeploymentStatus] @go(Nested)
	source?: string @go(Source)
}

// #StatusSubstrateRequest — the host-collection request the command:status plugin sends over
// HostBuild("status-substrate"). single=true selects the pod-scoped detail path (box+instance);
// otherwise the full multi-substrate fan-out (include_all mirrors --all, nested mirrors --nested).
#StatusSubstrateRequest: {
	single?:      bool   @go(Single)
	include_all?: bool   @go(IncludeAll)
	nested?:      bool   @go(Nested)
	box?:         string @go(Box)
	instance?:    string @go(Instance)
}

// #StatusNestedNode — one pre-resolved node of the DECLARED nested tree. The host resolves
// everything the candy's PURE overlay needs (kind via classifyTarget, the flat-row match keys via
// [dotted-path, NestedContainerName(dotted-path)], and — when nested was requested — the live
// probe result), so the candy folds WITHOUT any core type / ResolveDeployChain / classifyTarget.
// key is the declared child key (the Image cell). match_keys index the flat rows; for a ROOT node
// key itself is the flat match. RECURSIVE self-reference mirrors the deploy tree nesting.
#StatusNestedNode: {
	key:          string          @go(Key)
	path:         string          @go(Path)
	kind:         #SubstrateKind  @go(Kind,type=SubstrateKind)
	has_children: bool            @go(HasChildren)
	match_keys?: [...string]      @go(MatchKeys)
	live_status?: string          @go(LiveStatus)
	children?: [...#StatusNestedNode] @go(Children)
}

// #StatusSubstrateReply — the host-collection result: the flat rows (all substrates, already
// probed), the pre-resolved declared roots (only roots with children matter), and — on the single
// path — the one detail row. The candy applies the PURE overlay(rows, roots) then renders.
#StatusSubstrateReply: {
	rows?: [...#DeploymentStatus]   @go(Rows)
	roots?: [...#StatusNestedNode]  @go(Roots)
	single?: #DeploymentStatus      @go(Single)
}
