// CUE schema for the OCI-label metadata hub (P2B: BoxMetadata → spec, task #60).
// #BoxMetadata is the runtime-relevant config a deploy extracts from an image's OCI
// labels (ExtractMetadata) — CUE-sourced here so the whole deploy resolution reads ONE
// spec-owned type instead of a hand struct in charly core. The charly home + the sub-shape
// homes (labels.go/alias_collect.go/secrets.go) alias onto these (SDD: wire types are
// CUE-sourced; the P8b-followup/P14a precedent). Package-less; concatenated into spec.
//
// R8 (the byte-freeze gate): #BoxMetadata itself is NEVER whole-struct-marshaled — a deploy
// builds it FIELD-BY-FIELD from ~45 individual labels (ExtractMetadata), so ITS tags are
// wire-irrelevant. The R8 anchor is the MARSHALED label sub-shapes below — their json tags
// must reproduce the hand-struct tags byte-for-byte (required `!` → no omitempty, optional
// `?` → omitempty). deploykit.VolumeMount + kit.LabelDescriptionSet relocate here so
// #BoxMetadata's Volume/Description fields resolve (both alias back at their homes).

// #VolumeMount — a resolved named-volume mount (charly-<deploy>-<name> → container path).
// In-memory only (never marshaled) → tags wire-irrelevant. deploykit.VolumeMount aliases this.
#VolumeMount: {
	volume_name?:    string @go(VolumeName)
	container_path?: string @go(ContainerPath)
}

// #LabeledDescription — one origin's plan-shaped self-description (MARSHALED into
// ai.opencharly.description). origin is REQUIRED (json:"origin", no omitempty). kit aliases it.
#LabeledDescription: {
	origin!:      string @go(Origin)
	description?: string @go(Description)
	plan?: [...#Step] @go(Plan)
}

// #LabelDescriptionSet — the three-section (candy/box/deploy) description manifest
// (ai.opencharly.description). kit.LabelDescriptionSet aliases this.
#LabelDescriptionSet: {
	candy?: [...#LabeledDescription] @go(Candy)
	box?: [...#LabeledDescription] @go(Box)
	deploy?: [...#LabeledDescription] @go(Deploy)
}

// #LabelVolumeEntry — a volume in the ai.opencharly.volume label (name+path both required).
#LabelVolumeEntry: {
	name!: string @go(Name)
	path!: string @go(Path)
}

// #LabelRouteEntry — a traefik route in ai.opencharly.route (host+port both required).
#LabelRouteEntry: {
	host!: string @go(Host)
	port!: int    @go(Port,type=int)
}

// #CapabilityService — the full structured service spec baked into ai.opencharly.service.
// name required; all else omitempty. Mirrors the hand struct byte-for-byte.
#CapabilityService: {
	name!:              string @go(Name)
	scope?:             string @go(Scope)
	enable?:            bool   @go(Enable)
	use_packaged?:      string @go(UsePackaged)
	exec?:              string @go(Exec)
	env?: {[string]: string} @go(Env)
	restart?:           string @go(Restart)
	working_directory?: string @go(WorkingDirectory)
	user?:              string @go(User)
	after?: [...string] @go(After)
	before?: [...string] @go(Before)
	stdout?:            string @go(Stdout)
	stop_timeout?:      string @go(StopTimeout)
	kind?:              string @go(Kind)
	events?:            string @go(Events)
	auto_start?:        bool   @go(AutoStart,optional=nillable)
	start_retries?:     int    @go(StartRetries,type=int)
	start_sec?:         int    @go(StartSec,type=int)
	stop_signal?:       string @go(StopSignal)
	exit_code?:         string @go(ExitCode)
	priority?:          int    @go(Priority,type=int)
	init?:              string @go(Init)
	candy?:             string @go(Candy)
}

// #CapabilityInitDef — the deploy-relevant subset of an InitDef (ai.opencharly.init_def).
#CapabilityInitDef: {
	entrypoint?: [...string] @go(Entrypoint)
	fallback_entrypoint?: [...string] @go(FallbackEntrypoint)
	management_tool?: string @go(ManagementTool)
	management_commands?: {[string]: string} @go(ManagementCommands)
}

// #LabelDataEntry — a data mapping in ai.opencharly.data (volume/staging/candy required).
#LabelDataEntry: {
	volume!:  string @go(Volume)
	staging!: string @go(Staging)
	candy!:   string @go(Candy)
	dest?:    string @go(Dest)
}

// #ShellEntry — one origin's shell-init contribution (in ai.opencharly.shell). origin required.
#ShellEntry: {
	origin!:  string @go(Origin)
	id?:      string @go(ID)
	generic?: #ShellSpec @go(Generic,optional=nillable)
	by_shell?: {[string]: #ShellSpec} @go(ByShell,type=map[string]*ShellSpec)
	priority?: int @go(Priority,type=int)
}

// #LabelShellSet — the three-section shell-init manifest (ai.opencharly.shell).
#LabelShellSet: {
	candy?: [...#ShellEntry] @go(Candy)
	box?: [...#ShellEntry] @go(Box)
	deploy?: [...#ShellEntry] @go(Deploy)
}

// #CollectedAlias — a command alias baked into ai.opencharly.alias (name+command required).
#CollectedAlias: {
	name!:    string @go(Name)
	command!: string @go(Command)
}

// #LabelSecretEntry — a secret requirement in ai.opencharly.secret (name+target required).
#LabelSecretEntry: {
	name!:   string @go(Name)
	target!: string @go(Target)
	env?:    string @go(Env)
}

// #BoxMetadata — the OCI-label metadata hub. NEVER whole-marshaled (ExtractMetadata builds it
// field-by-field), so its own tags are wire-irrelevant. Deploy-only fields (Tunnel/DNS/
// AcmeEmail/Engine) are fed by MergeDeployOntoMetadata, never baked. PortProto RESHAPED
// map[int]string → map[string]string (the WIRE was always string-keyed; the int-key was an
// ExtractMetadata-internal convenience — resolveProto + ExtractMetadata rewired to match).
#BoxMetadata: {
	box?:       string @go(Box)
	version?:   string @go(Version)
	registry?:  string @go(Registry)
	bootc?:     bool   @go(Bootc)
	uid?:       int    @go(UID,type=int)
	gid?:       int    @go(GID,type=int)
	user?:      string @go(User)
	home?:      string @go(Home)
	port?: [...string] @go(Port)
	volume?: [...#VolumeMount] @go(Volume)
	alias?: [...#CollectedAlias] @go(Alias)
	security?: #Security @go(Security)
	network?:   string @go(Network)
	tunnel?: {...} @go(Tunnel,type=*TunnelYAML)
	dns?:        string @go(DNS)
	acme_email?: string @go(AcmeEmail)
	env?: [...string] @go(Env)
	hook?: #CandyHook @go(Hook,optional=nillable)
	route?: [...#LabelRouteEntry] @go(Route)
	init?:     string @go(Init)
	init_def?: #CapabilityInitDef @go(InitDef,optional=nillable)
	service?: [...#CapabilityService] @go(Service)
	service_names?: [...string] @go(ServiceNames)
	env_candy?: {[string]: string} @go(EnvCandy)
	path_append?: [...string] @go(PathAppend)
	engine?:    string @go(Engine)
	port_proto?: {[string]: string} @go(PortProto)
	port_relay?: [...int] @go(PortRelay,type=[]int)
	skill?:     string @go(Skill)
	status?:    string @go(Status)
	info?:      string @go(Info)
	candy_version?: {[string]: string} @go(CandyVersion)
	secret?: [...#LabelSecretEntry] @go(Secret)
	distro?: [...string] @go(Distro)
	build_format?: [...string] @go(BuildFormat)
	builder?: {[string]: string} @go(Builder)
	build?: [...string] @go(Build)
	data_entries?: [...#LabelDataEntry] @go(DataEntries)
	data_image?: bool @go(DataImage)
	env_provide?: {[string]: string} @go(EnvProvide)
	env_require?: [...#EnvDependency] @go(EnvRequire)
	env_accept?: [...#EnvDependency] @go(EnvAccept)
	secret_accept?: [...#EnvDependency] @go(SecretAccept)
	secret_require?: [...#EnvDependency] @go(SecretRequire)
	mcp_provide?: [...#CandyMCPProvide] @go(MCPProvide)
	mcp_require?: [...#EnvDependency] @go(MCPRequire)
	mcp_accept?: [...#EnvDependency] @go(MCPAccept)
	description?: #LabelDescriptionSet @go(Description,optional=nillable)
	shell?: #LabelShellSet @go(Shell,optional=nillable)
	check_level?: string @go(CheckLevel)
}
