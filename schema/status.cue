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

// #StatusCollectInput — the host<->command:status collect-seam request. The command:status
// candy owns the `charly status` CLI grammar + render; the fleet COLLECT (podman/libvirt/ledger/
// adb/k8s enumeration) stays a host-side mechanism the candy reaches over the reverse channel
// (HostBuild("status-collect")), mirroring command:vm's HostBuild("vm-build"). box="" → the
// all-substrate fleet view (Collector.All); a non-empty box → the single-deployment detail
// (Collector.Single). all mirrors --all, nested mirrors --nested.
#StatusCollectInput: {
	box?:      string @go(Box)
	instance?: string @go(Instance)
	all?:      bool   @go(All)
	nested?:   bool   @go(Nested)
}

// #StatusCollectReply — the collected rows (already merged, nested-overlaid, and sorted host-side).
// The candy renders them (table / json / detail) from spec.DeploymentStatus. A single-deployment
// request returns exactly one row.
#StatusCollectReply: {
	rows?: [...#DeploymentStatus] @go(Rows)
}
