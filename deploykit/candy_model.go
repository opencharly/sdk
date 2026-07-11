package deploykit

import "github.com/opencharly/sdk/vmshared"

// candy_model.go — CandyModel, the read-only view of a runtime candy that the
// deploy-plan compiler (BuildDeployPlan) consumes. The concrete runtime Candy lives
// in charly and implements this interface; the compiler depends on the abstraction
// (kernel/plugin boundary law: a Mechanism consumes an interface, the concrete kind
// implements it), so the candy struct + its whole test suite stay in charly with
// zero cross-package churn.
type CandyModel interface {
	GetName() string
	GetSourceDir() string
	GetVersion() string
	Vars() map[string]string
	PlanSteps() []Step
	Reboot() bool
	Apk() []ApkPackageSpec
	EnvConfig() (*EnvConfig, error)
	Service() []ServiceEntry
	Shell() *ShellConfig
	TopPackages() []string
	FormatSection(name string) *PackageSection
	LocalPkg(format string) string
	TagSection(tag string) *TagPkgConfig
	HasFile(filename string) bool

	// P8 render-delta accessors (build-mode graph/render surface). Field-backed
	// members use Get* to avoid a field/method name collision on the charly *Candy
	// implementer; the three already-methods keep their names.
	GetIncludedCandy() []CandyRef // candy: composition refs (splicing)
	GetRequire() []CandyRef       // require: deps (ordering + resolution)
	HasContent() bool
	PixiManifest() string
	GetHasPackageJson() bool
	GetHasCargoToml() bool
	HasFormatPackages() bool
	GetRemote() bool // true if the candy came from a remote repo
	HasExtract() bool
	Extract() []ExtractYAML
	HasData() bool
	Data() []DataYAML
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
	HasTasks() bool        // the candy has any tasks: (runOps non-empty)
	RunOps() []vmshared.Op // the candy's plan lowered to build-mode run ops
}
