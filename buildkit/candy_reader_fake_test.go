package buildkit

import "github.com/opencharly/sdk/spec"

// candy_reader_fake_test.go — a minimal spec.CandyReader test double shared by every
// buildkit test that needs to hand a candy to status.go / capabilities.go / init_config.go.
// buildkit cannot import deploykit's specCandyAdapter (deploykit already imports buildkit —
// that would be an import cycle), so this package keeps its own tiny double rather than a
// hand-duplicated copy of the full adapter (R3: only the accessors buildkit's own functions
// actually read are configurable; every other CandyReader method returns its zero value,
// which is correct because none of the functions under test in this package touch them).
type fakeCandyReader struct {
	name         string
	status       string
	caps         *spec.CandyCapability
	requiresCaps []string
	hasInit      map[string]bool
	relayPorts   []int
}

func (f *fakeCandyReader) GetName() string                     { return f.name }
func (f *fakeCandyReader) GetSourceDir() string                { return "" }
func (f *fakeCandyReader) GetVersion() string                  { return "" }
func (f *fakeCandyReader) Vars() map[string]string             { return nil }
func (f *fakeCandyReader) PlanSteps() []spec.Step              { return nil }
func (f *fakeCandyReader) Reboot() bool                        { return false }
func (f *fakeCandyReader) Apk() []spec.ApkPackageSpec          { return nil }
func (f *fakeCandyReader) EnvConfig() (*spec.EnvConfig, error) { return nil, nil }
func (f *fakeCandyReader) Service() []spec.CandyService        { return nil }
func (f *fakeCandyReader) Shell() *spec.Shell                  { return nil }
func (f *fakeCandyReader) TopPackages() []string               { return nil }
func (f *fakeCandyReader) FormatSection(name string) *spec.PackageSection {
	return nil
}
func (f *fakeCandyReader) LocalPkg(format string) string            { return "" }
func (f *fakeCandyReader) TagSection(tag string) *spec.TagPkgConfig { return nil }
func (f *fakeCandyReader) HasFile(filename string) bool             { return false }

func (f *fakeCandyReader) GetIncludedCandy() []spec.CandyRefEntry { return nil }
func (f *fakeCandyReader) GetRequire() []spec.CandyRefEntry       { return nil }
func (f *fakeCandyReader) GetBakePlugin() []spec.CandyRefEntry    { return nil }
func (f *fakeCandyReader) HasContent() bool                       { return false }
func (f *fakeCandyReader) HasInstallFiles() bool                  { return false }
func (f *fakeCandyReader) PixiManifest() string                   { return "" }
func (f *fakeCandyReader) GetHasPackageJson() bool                { return false }
func (f *fakeCandyReader) GetHasCargoToml() bool                  { return false }
func (f *fakeCandyReader) GetExternalBuilder() string             { return "" }
func (f *fakeCandyReader) HasFormatPackages() bool                { return false }
func (f *fakeCandyReader) GetRemote() bool                        { return false }
func (f *fakeCandyReader) HasExtract() bool                       { return false }
func (f *fakeCandyReader) Extract() []spec.CandyExtract           { return nil }
func (f *fakeCandyReader) HasData() bool                          { return false }
func (f *fakeCandyReader) Data() []spec.CandyData                 { return nil }
func (f *fakeCandyReader) GetHasPixiLock() bool                   { return false }
func (f *fakeCandyReader) GetRepoPath() string                    { return "" }
func (f *fakeCandyReader) GetSubPathPrefix() string               { return "" }
func (f *fakeCandyReader) HasEnv() bool                           { return false }
func (f *fakeCandyReader) HasRoute() bool                         { return false }
func (f *fakeCandyReader) Route() (*spec.RouteConfig, error)      { return nil, nil }

func (f *fakeCandyReader) HasInit(initName string) bool { return f.hasInit[initName] }
func (f *fakeCandyReader) ServiceFiles() []string       { return nil }
func (f *fakeCandyReader) RelayPorts() []int            { return f.relayPorts }

func (f *fakeCandyReader) HasTasks() bool    { return false }
func (f *fakeCandyReader) RunOps() []spec.Op { return nil }

func (f *fakeCandyReader) Security() *spec.Security            { return nil }
func (f *fakeCandyReader) Hooks() *spec.CandyHook              { return nil }
func (f *fakeCandyReader) EnvRequire() []spec.EnvDependency    { return nil }
func (f *fakeCandyReader) EnvAccept() []spec.EnvDependency     { return nil }
func (f *fakeCandyReader) SecretRequire() []spec.EnvDependency { return nil }
func (f *fakeCandyReader) SecretAccept() []spec.EnvDependency  { return nil }
func (f *fakeCandyReader) MCPRequire() []spec.EnvDependency    { return nil }
func (f *fakeCandyReader) MCPAccept() []spec.EnvDependency     { return nil }

func (f *fakeCandyReader) Alias() []spec.CandyAlias            { return nil }
func (f *fakeCandyReader) HasAliases() bool                    { return false }
func (f *fakeCandyReader) Volume() []spec.CandyVolume          { return nil }
func (f *fakeCandyReader) HasVolumes() bool                    { return false }
func (f *fakeCandyReader) Artifact() []spec.CandyArtifact      { return nil }
func (f *fakeCandyReader) Capabilities() *spec.CandyCapability { return f.caps }
func (f *fakeCandyReader) RequiresCapabilities() []string      { return f.requiresCaps }
func (f *fakeCandyReader) Engine() string                      { return "" }
func (f *fakeCandyReader) EnvProvides() map[string]string      { return nil }
func (f *fakeCandyReader) MCPProvide() []spec.CandyMCPProvide  { return nil }
func (f *fakeCandyReader) Secret() []spec.CandySecret          { return nil }
func (f *fakeCandyReader) Port() ([]string, error)             { return nil, nil }
func (f *fakeCandyReader) PortSpecs() []spec.PortSpec          { return nil }
func (f *fakeCandyReader) LocalPkgFormats() []string           { return nil }
func (f *fakeCandyReader) HasEnvAccepts() bool                 { return false }
func (f *fakeCandyReader) HasEnvProvides() bool                { return false }
func (f *fakeCandyReader) HasEnvRequires() bool                { return false }
func (f *fakeCandyReader) HasMCPAccepts() bool                 { return false }
func (f *fakeCandyReader) HasMCPProvides() bool                { return false }
func (f *fakeCandyReader) HasMCPRequires() bool                { return false }
func (f *fakeCandyReader) HasPorts() bool                      { return false }
func (f *fakeCandyReader) HasSecretAccepts() bool              { return false }
func (f *fakeCandyReader) HasSecretRequires() bool             { return false }

func (f *fakeCandyReader) IsPluginCandy() bool          { return false }
func (f *fakeCandyReader) GetPluginSource() string      { return "" }
func (f *fakeCandyReader) GetPluginProviders() []string { return nil }

func (f *fakeCandyReader) GetStatus() string      { return f.status }
func (f *fakeCandyReader) GetDescription() string { return "" }

func (f *fakeCandyReader) AgentProvide() []spec.AgentRuntimeCapability { return nil }
func (f *fakeCandyReader) HasAgentProvides() bool                      { return false }
func (f *fakeCandyReader) TerminalProfiles() map[string]spec.TerminalProfile {
	return nil
}

var _ spec.CandyReader = (*fakeCandyReader)(nil)
