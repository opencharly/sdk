// CUE schema for the `deploy` AND `check` kinds. Both validate ONE
// BundleNode (charly/deploy.go): a `deploy:` map entry, or a `kind: check`
// bed (disposable:true + usually iterate:/plan:). #Deploy is the base node;
// #Check narrows it to the bed invariants. CLOSED. Shared defs REFERENCED, not
// redefined (R3): #Step/#Op/#Security/#InstallOpts/#Duration/#CalVer/
// #EntityRef/#PortPin/#VmSize/#Sidecar/#ShellSpec live in _common.cue / sidecar.cue.

// #DeployTraits is a SUBSTRATE kind's DECLARED deploy behaviour (P9): a substrate plugin
// advertises it per word over Describe (ProvidedCapability.deploy_traits), and kit.StampDescent
// stamps it onto every node's #DescentDescriptor. It is the SINGLE plugin-declared source for
// "how does this substrate behave in the deploy chain", so the kernel consults the traits off
// node.Descent BY TRAIT — never by switching on the substrate kind word (the boundary law).
// Canonical table: pod=container+image_backed+image_context; vm=ssh+machine_venue+exclusive_venue;
// local=shell+machine_venue; k8s=shell+image_context+leaf_only; android=parent; zero value =
// external-in-place. Not authored — charly-written state stamped at load.
#DeployTraits: {
	// venue: how commands physically execute in this substrate's venue:
	//   container — podman/docker exec into the container by name (pod).
	//   ssh       — an ssh hop into the guest (vm).
	//   shell     — the substrate's own root executor runs on the host (local; k8s host-side).
	//   parent    — reached via the parent's venue, no own executor (android).
	//   none      — external-in-place (zero value / group).
	venue?: ("container" | "ssh" | "shell" | "parent" | "none") @go(Venue, type=string)
	// image_backed: the substrate runs a baked OCI image (pod).
	image_backed?: bool @go(ImageBacked)
	// image_context: the substrate composes over an image build context (pod overlay, k8s manifests).
	image_context?: bool @go(ImageContext)
	// machine_venue: the substrate is a full machine with a system init (host/vm/local) — its
	// services render as systemd units, not a container init.
	machine_venue?: bool @go(MachineVenue)
	// exclusive_venue: the substrate holds an exclusive host-resource lease boundary (vm).
	exclusive_venue?: bool @go(ExclusiveVenue)
	// leaf_only: the substrate is a deploy-chain LEAF — it cannot be descended into (k8s).
	leaf_only?: bool @go(LeafOnly)
}

// #DescentDescriptor is the loader-DERIVED venue-hop descriptor (Cutover H + P9).
// candy/plugin-substrate declares its per-word #DeployTraits over Describe, and kit.StampDescent
// stamps them here so the deploy chain descends generically by TRANSPORT and every consult site
// reads the substrate behaviour off the stamped TRAITS — never by switching on the substrate
// kind word. transport/host_rooted are the DERIVED nesting view (from the traits); the closed
// transport set is the kernel's nesting-boundary vocabulary.
#DescentDescriptor: {
	// transport: how the deploy chain descends INTO this node's substrate (DERIVED from the
	// declared venue + leaf_only by kit.DescentFromTraits):
	//   none           — shares the parent venue; no hop (local, android).
	//   container-exec — enter the container by name (pod; podman/docker per engine).
	//   ssh            — an ssh hop into the guest (vm).
	//   reject         — unreachable via the deploy chain (k8s → use kubectl).
	transport: "none" | "container-exec" | "ssh" | "reject"
	// host_rooted: the substrate's own ROOT executor runs directly on the host
	// (local), so the check runner uses rootExecutorForDeployNode, not a container chain.
	host_rooted?: bool @go(HostRooted)
	// the plugin-DECLARED substrate traits (P9), stamped in by kit.StampDescent — the consult
	// sites read these instead of switching on the substrate kind word.
	#DeployTraits
}

#Deploy: {
	version?:     #CalVer
	description?: string & !=""

	// target is DERIVED from the node's discriminator kind + cross-ref at load
	// (buildBundleNode/inferBundleTarget) — NOT authored in node-form. Optional
	// here so #Check's arms can pin it; the #BundleValue arm (node.cue) rejects an
	// authored `target:` outright. The former default `*"pod"` is dropped (Go's
	// classifyTarget supplies the empty→pod default). Generated as a plain Go
	// `string` (the loader stamps it; the CUE enum still validates a pinned value).
	target?: ("pod" | "vm" | "k8s" | "local" | "android") @go(Target,type=string) // loader-DERIVED (yaml:"-")

	// member_of + inside are loader-DERIVED runtime fields (never authored;
	// rejected by #BundleValue): member_of marks a folded sibling-member entry,
	// inside names the venue a nested resource deploys into. Generated for the Go
	// tree-walker, forbidden in authoring.
	member_of?: string @go(MemberOf)
	inside?:    string @go(Inside)

	// agent_provisioned marks a resource member/child the AI deploys at run time
	// (the iterate-benchmark contract): image-less (no box:), not folded to a
	// top-level entry, exempt from the box-required validators. See deploy.go.
	agent_provisioned?: bool @go(AgentProvisioned)

	// descent is the loader-DERIVED venue-hop descriptor (the descent de-type,
	// Cutover H): the substrate plugin stamps it at OpLoad (via kit.StampDescent) so
	// the deploy chain (AppendHopForFlatPath) descends generically BY TRANSPORT,
	// never by switching on the substrate kind word. charly-written state, never
	// authored (rejected by #BundleValue).
	descent?: #DescentDescriptor @go(Descent,optional=nillable)

	// EDGE-INHERIT cutover B: the substrate kind is the EDGE discriminator (pod:/vm:/
	// k8s:/local:/android:/group:), so the deploy carries only NON-kind cross-refs:
	//   from  — inherit a SAME-kind template by name (vm/k8s/local/android deploys).
	//   image — the box/OCI artifact a pod/k8s/android RUNS (the former `box:`).
	// Per-substrate validity (image⊻from, source⊻from) is enforced in Go
	// (classifyTarget / validateDeploy), not CUE, so a `vm:` node is a VmSpec template
	// (source:) OR a deploy (from:) under ONE arm — the disjunction #Vm|#Deploy.
	from?:  #EntityRef
	image?: string & !=""

	kind?:     "service" | "daemon" | "batch" | "scheduled" | "oneshot"
	replica?:  int & >=0 @go(,type=int)
	restart?:  "always" | "on-failure" | "never"
	schedule?: string & !=""

	tunnel?:     #Tunnel       @go(Tunnel,type=*TunnelYAML)
	dns?:        string & !="" @go(DNS)
	acme_email?: string & !="" @go(AcmeEmail)
	port?: [...#PortPin]
	resolved_port?: [...#PortPin] @go(ResolvedPort)
	// resolved_image: the concrete image ref the pod deploy's add_candy: overlay
	// build produced (`<deploy-key>-overlay:<hash>`), persisted by PrepareVenue so
	// config/start deploy EXACTLY that overlay (carrying the add_candy layers)
	// instead of re-resolving the base image: short-name by a CalVer sort that the
	// overlay alias can lose to the base on a same-minute build. charly-written
	// state (like resolved_port), never authored; empty for a plain pod.
	resolved_image?: string & !="" @go(ResolvedImage)
	env?: {PATH?: _|_, [string]: #StrVal} @go(Env,type=map[string]string)
	env_file?: string & !="" @go(EnvFile)
	network?:  string & !=""
	engine?:   "podman" | "docker"
	security?: #Security @go(Security,optional=nillable)
	secret?: [...#DeploySecret]
	volume?: [...#DeployVolume]
	// The Go type is OPAQUE (map[string]json.RawMessage): CUE still validates each
	// per-deploy sidecar override against #Sidecar, but the kernel stores it as
	// opaque bodies — ALL sidecar business logic lives in candy/plugin-sidecar's
	// OpResolve (the sidecar de-type, Cutover D). The host never reads Sidecar fields.
	sidecar?: {[string]: #Sidecar} @go(Sidecar,type=map[string]RawBody)
	forward_gpg_agent?: bool @go(ForwardGpgAgent,type=*bool)
	forward_ssh_agent?: bool @go(ForwardSshAgent,type=*bool)

	plan?: [...#Step]
	iterate?: #Iterate @go(Iterate,optional=nillable)
	shell?: [...#DeployShellOverlay]

	add_candy?: [...(string & !="")] @go(AddCandy)
	install_opts?: #InstallOpts @go(InstallOpts,optional=nillable)

	host?: string & !=""
	user?: string & !=""
	ssh_arg?: [...string] @go(SSHArgs)

	cpus?:      int & >=1 @go(,type=int)
	ram?:       #VmSize
	disk_size?: #VmSize @go(DiskSize)

	kubernetes?: #K8sDeploy @go(Kubernetes,optional=nillable)

	resources?: #DeployResources @go(Resources,optional=nillable)
	expose?:    #DeployExpose    @go(Expose,optional=nillable)
	storage?: [...#DeployStorage]
	probes?: #DeployProbes @go(Probes,optional=nillable)

	from_snapshot?:    string & !=""  @go(FromSnapshot)
	cloud_init_clean?: bool           @go(CloudInitClean)
	vm_state?:         #VmDeployState @go(VmState,type=*VmDeployState)

	disposable?:  bool @go(,type=*bool)
	lifecycle?:   "scratch" | "dev" | "test" | "qa" | "staging" | "prod" | "custom"
	ephemeral?:   #Ephemeral   @go(Ephemeral,type=*EphemeralLifetime)
	preemptible?: #Preemptible @go(Preemptible,type=*PreemptibleConfig)
	requires_exclusive?: [...(string & !="")] @go(RequiresExclusive)
	requires_shared?: [...(string & !="")] @go(RequiresShared)

	// nested/peer map keys carry no dots (validateDeploymentName). Loader-built
	// runtime tree maps (Children = nested-inside venue; Members = brought-up
	// alongside on the shared network) — gengotypes can't express a pattern-keyed
	// self-referential map, so the Go type is pinned explicitly.
	nested?: {[=~"^[^.]+$"]: #Deploy} @go(Children,type=map[string]*Deploy)
	peer?: {[=~"^[^.]+$"]: #Deploy} @go(Members,type=map[string]*Deploy)
}

// #Check — a kind:check bed. Structurally IDENTICAL to #Deploy (same BundleNode
// Go struct), so it is a plain reference (R3 — no field duplication; stays CLOSED
// because #Deploy is closed). The bed-mode invariants the former `& (A|B|C)`
// disjunction expressed — disposable required + bed-legal target ∈ {pod,vm,local,
// android} for the deterministic/ephemeral modes, the iterate AI-benchmark mode,
// and the ephemeral⇒disposable promotion — are enforced in GO at load time
// (validateCheckBeds + validateEphemeralUnified in unified.go), which is the
// SINGLE source of truth for the actual bundle-form beds (a node-form check bed
// is a `bundle:` node validated via #BundleValue=#Deploy, so the disjunction was
// only ever applied to the legacy root-shape `check:` collection). Relaxing it to
// the alias removes that divergent parallel spec and lets gengotypes emit a real
// Check struct instead of an empty `struct{}`.
#Check: #Deploy

#Tunnel: (("tailscale" | "cloudflare") | {
		provider: "tailscale" | "cloudflare"
		tunnel?:  string & !=""
		public?:  #PortScope
		private?: #PortScope
}) @go(-) // gengotypes: hand TunnelYAML (spec/union_types.go)

// PortScope (tunnel.go): "all" scalar | a list of container ports | a
// port→hostname map (PortMap). Ports are ints; hostnames strings.
#PortScope: ("all" | [...int] | {[=~"^[0-9]+$"]: string}) @go(-) // gengotypes: hand PortScope

// DeployShellOverlay (deploy.go) — per-deploy shell-rc overlay: the shared
// #Shell body extended with the overlay identity/skip keys (id-keyed
// replace / skip merge semantics, MergeDeployShell).
#DeployShellOverlay: {
	#Shell
	id?:     string & !="" @go(ID)
	origin?: string & !=""
	skip?:   bool
}

// DeployProbes (deploy.go) — each probe is an inline Op (the check verb vocab).
#DeployProbes: {
	liveness?:  #Op @go(Liveness,optional=nillable)
	readiness?: #Op @go(Readiness,optional=nillable)
	startup?:   #Op @go(Startup,optional=nillable)
}

// VmDeployState (deploy.go) — MACHINE-WRITTEN runtime state, never authored.
// Forward-evolving; the open `...` tail is the justified hatch for a state
// record — which makes gengotypes degrade the whole def to `map[string]any`.
// The hand Go field is a CONCRETE *VmDeployState struct (with its own nested
// state sub-types), so @go(-) suppresses the lossy map type and the faithful
// VmDeployState is hand-written in spec/union_types.go (the field references it
// via @go(VmState,type=*VmDeployState)). Runtime ingress validation still uses
// this open #VmDeployState — @go(-) only affects the generated Go type.
#VmDeployState: {
	instance_id?:                string
	disk_path?:                  string
	seed_iso?:                   string
	ssh_port?:                   int
	ssh_user?:                   string
	backend?:                    "auto" | "qemu" | "libvirt" // "auto" persisted pre-resolution (the vm deploy lifecycle hook)
	cloud_init_rendered_digest?: string
	charly_install_strategy?:    "auto" | "scp" | "url" | "skip"
	// port_forwards persists the auto-allocated host ports for `auto:<guest>`
	// network.port_forwards entries (guest-port → allocated host port), so the
	// allocation is reused across the vm-create → deploy-add sequence — the
	// sibling of ssh_port. Validation-only (the Go type is hand-mirrored, @go(-)).
	port_forwards?: {[string]: int}
	...
} @go(-)

#DeployVolume: {
	name:         string & !=""
	type?:        "volume" | "bind" | "encrypted"
	host?:        string & !=""
	path?:        string & =~"^/"
	data_seeded?: bool          @go(DataSeeded)
	data_source?: string & !="" @go(DataSource)
}
#DeploySecret: {
	name:    string & !=""
	source?: string & !=""
}
#DeployResources: {
	cpu_request?:    string & !="" @go(CPURequest)
	memory_request?: string & !="" @go(MemoryRequest)
}
#DeployExpose: {
	host?: string & !=""
	path?: string & =~"^/"
	tls?:  bool @go(TLS)
	port?: string & !=""
}
#DeployStorage: {
	name:        string & !=""
	size?:       string & !=""
	class_hint?: "fast" | "cheap" | "encrypted" | "default" @go(ClassHint)
	access?:     "single-writer" | "many-readers" | "many-writers"
	path?:       string & =~"^/"
}
#K8sDeploy: {
	namespace?: #EntityRef
	workload?:  "Deployment" | "StatefulSet" | "DaemonSet" | "Pod" | "Job" | "CronJob"
	patches?: [...#K8sPatch]
	raw?: [...string]
}
#K8sPatch: {
	target: {
		kind?:      string & !=""
		name?:      string & !=""
		namespace?: string & !=""
	}
	patch: string & !=""
}

#Ephemeral: (true | {
		ttl?:             #Duration
		keep_on_failure?: bool
		naming_pattern?:  string & !=""
}) @go(-) // gengotypes: hand EphemeralLifetime (spec/union_types.go)

#Preemptible: ([...(string & !="")] | {
	holds?: [...(string & !="")]
		stop?:    "shutdown"
		restore?: "always" | "on-success"
}) @go(-) // gengotypes: hand PreemptibleConfig
#Iterate: {
	agent?: [...(string & !="")]
	sandbox!:           string & !=""
	plateau_iteration?: int & >=0 @go(PlateauIteration,type=int)
	prompt?:            string
	note?:              bool @go(,type=*bool)
	env?:               #StrMap
	mcp_endpoint?:      string @go(MCPEndpoint,type=*string)
}
