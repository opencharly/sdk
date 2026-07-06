package spec

// init_wire.go — the OpResolve envelope for the init de-type (Cutover F).
// candy/plugin-init owns the init-system knowledge (supervisord/systemd assembly);
// the kernel stores init bodies opaquely and consumes RESOLVED fragments (rendered
// service units, emitted Containerfile init stages, the capability/entrypoint blob)
// via the plugin — it never reads spec.Init fields.

// KeyValue is a deterministic env-var ordering helper (template iteration order).
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ServiceRenderContext is the data an init system's service_template renders
// against — everything the renderer needs, nothing else reachable. The host
// computes it per service and hands it to candy/plugin-init's OpResolve.
type ServiceRenderContext struct {
	Name             string            `json:"name,omitempty"`
	Candy            string            `json:"candy,omitempty"`
	Exec             string            `json:"exec,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	EnvList          []KeyValue        `json:"env_list,omitempty"`
	Restart          string            `json:"restart,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	User             string            `json:"user,omitempty"`
	After            []string          `json:"after,omitempty"`
	Before           []string          `json:"before,omitempty"`
	WantedBy         []string          `json:"wanted_by,omitempty"`
	Stdout           string            `json:"stdout,omitempty"`
	StopTimeout      string            `json:"stop_timeout,omitempty"`
	Scope            string            `json:"scope,omitempty"`
	PackagedUnit     string            `json:"packaged_unit,omitempty"`
	Home             string            `json:"home,omitempty"`
	SystemUnitDir    string            `json:"system_unit_dir,omitempty"`
	UserUnitDir      string            `json:"user_unit_dir,omitempty"`
	FragmentDir      string            `json:"fragment_dir,omitempty"`

	// Lifecycle directives (supervisord + systemd).
	Kind         string `json:"kind,omitempty"`
	Events       string `json:"events,omitempty"`
	AutoStart    *bool  `json:"auto_start,omitempty"`
	StartRetries int    `json:"start_retries,omitempty"`
	StartSecs    int    `json:"start_secs,omitempty"`
	StopSignal   string `json:"stop_signal,omitempty"`
	ExitCodes    string `json:"exit_codes,omitempty"`
	Priority     int    `json:"priority,omitempty"`

	// RenderDropin is the host-precomputed drop-in decision (the entry carries
	// Overrides). PackagedUnit != "" selects the packaged branch. The host derives
	// both from the ServiceEntry so the plugin renders from the ctx alone.
	RenderDropin bool `json:"render_dropin,omitempty"`
}

// RenderedService is the renderer output: the unit text, where it lands, and any
// drop-in for packaged-unit reuse.
type RenderedService struct {
	UnitText   string `json:"unit_text,omitempty"`
	UnitPath   string `json:"unit_path,omitempty"`
	DropinText string `json:"dropin_text,omitempty"`
	DropinPath string `json:"dropin_path,omitempty"`
}

// ServiceRenderInput is candy/plugin-init's OpResolve input for the service-render
// leg: the OPAQUE init body (the chosen init system's config) + the host-built
// render context (all entry-derived fields, home-expanded, with the packaged/
// drop-in branch decisions precomputed). The plugin renders from the ctx alone.
type ServiceRenderInput struct {
	Init RawBody              `json:"init"`
	Ctx  ServiceRenderContext `json:"ctx"`
}

// ServiceRenderReply wraps the rendered service.
type ServiceRenderReply struct {
	Rendered *RenderedService `json:"rendered,omitempty"`
}

// ResolvedInit is the resolve-to-envelope form of an init system the kernel
// consumes for stage/assembly emission, capability labels, and the entrypoint
// contract (legs 2–4 of the init de-type). It carries the init system's build +
// runtime VALUES + templates the host reads generically — the kernel never imports
// the concrete spec.Init. Raw is the opaque init body threaded to the plugin's
// service-render leg (leg 1). candy/plugin-init produces it from spec.Init.
type ResolvedInit struct {
	CandyFields          []string          `json:"candy_field,omitempty"`
	CandyFiles           []string          `json:"candy_file,omitempty"`
	DependsCandy         string            `json:"depends_candy,omitempty"`
	RequiresCapability   []string          `json:"requires_capability,omitempty"`
	Model                string            `json:"model,omitempty"`
	HeaderFile           string            `json:"header_file,omitempty"`
	FragmentDir          string            `json:"fragment_dir,omitempty"`
	RelayTemplate        string            `json:"relay_template,omitempty"`
	StageName            string            `json:"stage_name,omitempty"`
	StageHeaderCopy      string            `json:"stage_header_copy,omitempty"`
	StageFragmentCopy    string            `json:"stage_fragment_copy,omitempty"`
	AssemblyTemplate     string            `json:"assembly_template,omitempty"`
	SystemEnableTemplate string            `json:"system_enable_template,omitempty"`
	PostAssemblyTemplate string            `json:"post_assembly_template,omitempty"`
	Entrypoint           []string          `json:"entrypoint,omitempty"`
	FallbackEntrypoint   []string          `json:"fallback_entrypoint,omitempty"`
	ManagementTool       string            `json:"management_tool,omitempty"`
	ManagementCommands   map[string]string `json:"management_command,omitempty"`
	LabelKey             string            `json:"label_key,omitempty"`
	ServiceSchema        *InitServiceSchema `json:"service_schema,omitempty"`

	// Raw is the opaque init body — threaded to the service-render leg (leg 1).
	Raw RawBody `json:"raw,omitempty"`
}

// InitResolveInput is candy/plugin-init's OpResolve config-leg input: the opaque
// init body to resolve into a ResolvedInit.
type InitResolveInput struct {
	Init RawBody `json:"init"`
}

// InitResolveReply wraps the resolved init.
type InitResolveReply struct {
	Resolved *ResolvedInit `json:"resolved,omitempty"`
}

// InitResolveRequest is the discriminated OpResolve input for candy/plugin-init:
// exactly one of Render (leg 1 — render one service unit) or Config (legs 2–4 —
// the resolved init envelope) is set.
type InitResolveRequest struct {
	Render *ServiceRenderInput `json:"render,omitempty"`
	Config *InitResolveInput   `json:"config,omitempty"`
}
