package deploykit

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
}
