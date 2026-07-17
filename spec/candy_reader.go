package spec

// candy_reader.go — CandyReader, the read-only view of a runtime candy that the
// deploy-plan compiler (deploykit.BuildDeployPlan) and the build-render engine consume. It lives
// in spec (the shared contract home, alongside the CUE-generated wire envelopes) rather than
// sdk/deploykit so an import-clean charly file can reach it WITHOUT crossing the mechanism-kit
// import-purity line — matches the DocParser/ProjectWalker precedent in loader_seam.go. The
// concrete runtime Candy (charly core) implements it; deploykit's specCandyAdapter implements it
// from the CUE-generated CandyModel/CandyView wire views. Every referenced type below is already
// spec-native (Step/EnvConfig/EnvDependency/Op/Security/CandyHook/CandyService/Shell/…) or promoted
// here alongside the interface (CandyRefEntry, below) — a spec-hosted contract never needs an sdk
// mechanism-kit import.
type CandyReader interface {
	GetName() string
	GetSourceDir() string
	GetVersion() string
	Vars() map[string]string
	PlanSteps() []Step
	Reboot() bool
	Apk() []ApkPackageSpec
	EnvConfig() (*EnvConfig, error)
	Service() []CandyService
	Shell() *Shell
	TopPackages() []string
	FormatSection(name string) *PackageSection
	LocalPkg(format string) string
	TagSection(tag string) *TagPkgConfig
	HasFile(filename string) bool

	// P8 render-delta accessors (build-mode graph/render surface). Field-backed
	// members use Get* to avoid a field/method name collision on the charly *Candy
	// implementer; the three already-methods keep their names.
	GetIncludedCandy() []CandyRefEntry // candy: composition refs (splicing)
	GetRequire() []CandyRefEntry       // require: deps (ordering + resolution)
	HasContent() bool
	HasInstallFiles() bool // at least one install file (drives pixi-bound detection)
	PixiManifest() string
	GetHasPackageJson() bool
	GetHasCargoToml() bool
	GetExternalBuilder() string // external_builder: word (the out-of-tree builder plugin this candy selects)
	HasFormatPackages() bool
	GetRemote() bool // true if the candy came from a remote repo
	HasExtract() bool
	Extract() []CandyExtract
	HasData() bool
	Data() []CandyData
	GetHasPixiLock() bool
	GetRepoPath() string
	GetSubPathPrefix() string
	HasEnv() bool
	HasRoute() bool
	Route() (*RouteConfig, error)

	// P8 init-cluster accessors (emitInitAssembly/emitInitFragmentStages/
	// generateInitFragments). RelayPorts wraps the field-backed PortRelayPorts to
	// avoid a field/method name collision on the charly *Candy implementer.
	HasInit(initName string) bool // this candy contributes to the named init system
	ServiceFiles() []string       // file_copy-model service unit paths (globbed)
	RelayPorts() []int            // port_relay: ports (init-agnostic)

	// P8 writeCandySteps accessors.
	HasTasks() bool // the candy has any tasks: (runOps non-empty)
	RunOps() []Op   // the candy's plan lowered to build-mode run ops

	// OCI-label-collector surface (CollectSecurity/CollectHooks/layer_secrets):
	// per-candy security/hooks + the six env/secret/mcp dependency lists.
	Security() *Security
	Hooks() *CandyHook
	EnvRequire() []EnvDependency
	EnvAccept() []EnvDependency
	SecretRequire() []EnvDependency
	SecretAccept() []EnvDependency
	MCPRequire() []EnvDependency
	MCPAccept() []EnvDependency

	// W9 mass-edit interface-completeness fill: the remaining accessors the 42-file
	// repoint needs (CollectBoxAlias/CollectBoxVolume/CollectLibvirtSnippets/ports.go/
	// layer_capabilities.go/…) — every one of these already exists on the charly *Candy
	// implementer (layers.go); this is a pure exposure widening, no new *Candy behavior.
	// Consumer-set audited: HasAnyInit/HasAnyPackages/HasApk/HasService/HasTagPackages
	// were considered and DROPPED — zero external caller outside layers.go itself (they
	// back OTHER *Candy-internal composition, e.g. HasContent/HasInstallFiles, which
	// stays a host-precomputed field per the #67 predicate-carrying pattern above).
	Alias() []CandyAlias
	HasAliases() bool
	Volume() []CandyVolume
	HasVolumes() bool
	Artifact() []CandyArtifact
	Capabilities() *CandyCapability
	RequiresCapabilities() []string
	Engine() string
	Libvirt() []string
	HasLibvirt() bool
	EnvProvides() map[string]string
	MCPProvide() []CandyMCPProvide
	Secret() []CandySecret
	Port() ([]string, error)
	PortSpecs() []PortSpec
	LocalPkgFormats() []string
	HasEnvAccepts() bool
	HasEnvProvides() bool
	HasEnvRequires() bool
	HasMCPAccepts() bool
	HasMCPProvides() bool
	HasMCPRequires() bool
	HasPorts() bool
	HasSecretAccepts() bool
	HasSecretRequires() bool
}
