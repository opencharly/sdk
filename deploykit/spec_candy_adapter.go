package deploykit

// spec_candy_adapter.go — the SERIALIZED-model adapter (task #60, Unit A). A resolving PLUGIN
// (validate route-i, the bundle-add walk) holds the resolved-project envelope — spec.CandyModel
// (build model) + spec.CandyView (identity/graph) per candy — but BuildDeployPlan / ResolveCandyOrder
// consume the deploykit.CandyModel INTERFACE (implemented in charly by the runtime *Candy). This
// adapter satisfies that interface from the two envelope views + the candy's SourceDir, so a plugin
// re-runs the deploy-plan compile + the resolution-graph checks WITHOUT the runtime *Candy graph
// (no materialize seam — it reads RESOLVED data). The filesystem-probe methods (HasFile / PixiManifest
// / GetHasPixiLock) resolve via live os.Stat against SourceDir (candy sources are on the same host;
// stdlib only — import-pure). Spike-proven (scratchpad/k3d-scoping-map.md §1(c)): 38/40 methods are
// direct field reads over the spec views, CandyRef is a bare-string conversion, and the DEPLOY walk
// touches only the snapshot-safe subset + HasFile + HasContent.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

// specCandyAdapter wraps the two envelope views + the source dir into a deploykit.CandyModel.
type specCandyAdapter struct {
	m  spec.CandyModel // build model (Plan / packages / services / env-deps / ...)
	v  spec.CandyView  // identity + graph (Require / IncludedCandy / Remote / RepoPath)
	sd string          // SourceDir (== m.SourceDir); anchor for live fs probes
}

// NewSpecCandyModel builds a deploykit.CandyModel from a candy's resolved-project envelope views.
// m + v are the CandyModels[name] / Candies[name] entries; SourceDir comes from m.
func NewSpecCandyModel(m spec.CandyModel, v spec.CandyView) CandyModel {
	return &specCandyAdapter{m: m, v: v, sd: m.SourceDir}
}

// RawCandy returns the underlying (CandyModel, CandyView) pair this adapter wraps — the escape
// hatch a caller that needs the RAW wire structs (not the narrower CandyReader method set) can
// reach via a type assertion (W9: charly core's ResolvedProject.CandyModels/.Candies are typed as
// raw struct maps, populated directly from the scan pipeline's own (m, v) — since ScanAllCandy's
// FINAL return is the wrapped spec.CandyReader, this is how the ONE caller that still needs the
// pre-wrap shape gets it back, without a parallel "raw scan" entry point or widening CandyReader
// with identity-only fields (Description/Status/Info/Plugin) no OTHER consumer needs).
func (a *specCandyAdapter) RawCandy() (spec.CandyModel, spec.CandyView) { return a.m, a.v }

// identity / scalars
func (a *specCandyAdapter) GetName() string      { return a.m.Name }
func (a *specCandyAdapter) GetSourceDir() string { return a.sd }
func (a *specCandyAdapter) GetVersion() string   { return a.m.Version }
func (a *specCandyAdapter) Reboot() bool         { return a.m.Reboot }
func (a *specCandyAdapter) GetRemote() bool      { return a.v.Remote }
func (a *specCandyAdapter) GetRepoPath() string  { return a.v.RepoPath }

// build-model direct field reads
func (a *specCandyAdapter) Vars() map[string]string { return a.m.Vars }
func (a *specCandyAdapter) PlanSteps() []Step       { return a.m.Plan }
func (a *specCandyAdapter) Apk() []ApkPackageSpec   { return a.m.Apk }
func (a *specCandyAdapter) TopPackages() []string   { return a.m.TopPackages }
func (a *specCandyAdapter) ServiceFiles() []string  { return a.m.ServiceFiles }
func (a *specCandyAdapter) RelayPorts() []int       { return a.m.PortRelayPorts }
func (a *specCandyAdapter) RunOps() []vmshared.Op   { return a.m.RunOps }
func (a *specCandyAdapter) HasTasks() bool          { return len(a.m.RunOps) > 0 }
func (a *specCandyAdapter) HasExtract() bool        { return len(a.m.Extract) > 0 }
func (a *specCandyAdapter) Extract() []ExtractYAML  { return a.m.Extract }
func (a *specCandyAdapter) HasData() bool           { return len(a.m.Data) > 0 }
func (a *specCandyAdapter) Data() []DataYAML        { return a.m.Data }

// maps / pointers
func (a *specCandyAdapter) LocalPkg(format string) string { return a.m.LocalPkg[format] }
func (a *specCandyAdapter) FormatSection(name string) *PackageSection {
	if s, ok := a.m.FormatSections[name]; ok {
		return &s
	}
	return nil
}
func (a *specCandyAdapter) TagSection(tag string) *TagPkgConfig {
	if s, ok := a.m.TagSections[tag]; ok {
		return &s
	}
	return nil
}
func (a *specCandyAdapter) HasEnv() bool                   { return a.m.Env != nil }
func (a *specCandyAdapter) EnvConfig() (*EnvConfig, error) { return a.m.Env, nil }
func (a *specCandyAdapter) HasRoute() bool                 { return a.m.Route != nil }
func (a *specCandyAdapter) Route() (*RouteConfig, error)   { return a.m.Route, nil }
func (a *specCandyAdapter) Shell() *ShellConfig            { return a.m.Shell }
func (a *specCandyAdapter) Service() []ServiceEntry        { return a.m.Service }

// graph refs (spec.CandyRef is a bare-string alias -> deploykit.CandyRef)
func (a *specCandyAdapter) GetIncludedCandy() []CandyRef { return specRefsToDk(a.v.IncludedCandy) }
func (a *specCandyAdapter) GetRequire() []CandyRef       { return specRefsToDk(a.v.Require) }
func (a *specCandyAdapter) GetBakePlugin() []CandyRef    { return specRefsToDk(a.m.BakePlugin) }

func specRefsToDk(in []spec.CandyRef) []CandyRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]CandyRef, len(in))
	for i, r := range in {
		out[i] = CandyRef{Raw: r}
	}
	return out
}

// live filesystem probes (SourceDir + os.Stat; stdlib only, import-pure)
func (a *specCandyAdapter) HasFile(filename string) bool { return a.fileExists(filename) }
func (a *specCandyAdapter) GetHasPackageJson() bool      { return a.fileExists("package.json") }
func (a *specCandyAdapter) GetHasCargoToml() bool        { return a.fileExists("Cargo.toml") }
func (a *specCandyAdapter) GetHasPixiLock() bool         { return a.fileExists("pixi.lock") }
func (a *specCandyAdapter) PixiManifest() string {
	for _, f := range []string{"pixi.toml", "pyproject.toml", "environment.yml"} {
		if a.fileExists(f) {
			return f
		}
	}
	return ""
}
func (a *specCandyAdapter) fileExists(name string) bool {
	if a.sd == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(a.sd, name))
	return err == nil
}

// derived predicates (aggregate of snapshot-safe fields)
func (a *specCandyAdapter) HasFormatPackages() bool {
	return len(a.m.FormatSections) > 0 || len(a.m.TagSections) > 0 || len(a.m.TopPackages) > 0
}
func (a *specCandyAdapter) HasContent() bool {
	// Host-precomputed (#67): the live *Candy.HasContent() verdict (env/ports/route/volumes/
	// aliases/libvirt/init + fs-probe caches the envelope cannot recompute faithfully), carried
	// on the CandyModel so the candy-graph composition matches the pre-move core render.
	return a.m.HasContent
}
func (a *specCandyAdapter) HasInstallFiles() bool {
	// Host-precomputed (#67): the live *Candy.HasInstallFiles() verdict (packages + fs-probe
	// detection + tasks/apk), carried on the CandyModel — distinct from HasContent (the pixi-
	// bound intermediate detection gates on this narrower predicate).
	return a.m.HasInstallFiles
}

// HasInit reports whether this candy triggers the NAMED init system — the per-init-system lookup
// (W9 finding): CandyView.InitSystems is the host-completed cross-candy map PopulateCandyInitSystem
// (charly) / its loaderkit port populates once, after every candy in the project is scanned,
// mirroring the live *Candy.HasInit(initName) exactly. Previously approximated via ServiceFiles/
// Service non-empty (ignoring initName entirely) because the envelope carried no per-init field —
// that approximation silently mismatched the live *Candy for any consumer asking about a SPECIFIC
// init system (e.g. this package's own EmitInitFragmentStages).
func (a *specCandyAdapter) HasInit(initName string) bool {
	return a.v.InitSystems[initName]
}

func (a *specCandyAdapter) GetExternalBuilder() string { return a.m.ExternalBuilder }

// Identity-view scalars (W9): widely-needed enough for a real interface method — see the
// CandyReader interface doc.
func (a *specCandyAdapter) GetStatus() string      { return a.v.Status }
func (a *specCandyAdapter) GetDescription() string { return a.v.Description }

// GetSubPathPrefix — the parent dir within the repo for a remote candy's COPY-source
// (RepoPath + SubPathPrefix + ref). Filled on the CandyView by the resolve projector (#67
// build-render move); the build-mode render (candyCopySource) reads it to reproduce remote
// COPY sources WITHOUT the live *Candy. (Was stubbed "" while build-mode was host-only.)
func (a *specCandyAdapter) GetSubPathPrefix() string { return a.v.SubPathPrefix }

// OCI-label-collector surface (CollectSecurity/CollectHooks/layer_secrets): direct
// field reads over the build model, same snapshot-safe shape as every other
// CandyModel field above.
func (a *specCandyAdapter) Security() *SecurityConfig      { return a.m.Security }
func (a *specCandyAdapter) Hooks() *HooksConfig            { return a.m.Hook }
func (a *specCandyAdapter) EnvRequire() []EnvDependency    { return a.m.EnvRequire }
func (a *specCandyAdapter) EnvAccept() []EnvDependency     { return a.m.EnvAccept }
func (a *specCandyAdapter) SecretRequire() []EnvDependency { return a.m.SecretRequire }
func (a *specCandyAdapter) SecretAccept() []EnvDependency  { return a.m.SecretAccept }
func (a *specCandyAdapter) MCPRequire() []EnvDependency    { return a.m.MCPRequire }
func (a *specCandyAdapter) MCPAccept() []EnvDependency     { return a.m.MCPAccept }

// W9 mass-edit interface-completeness fill: the 42-file repoint's remaining accessors.
// Alias/Volume/EnvProvides/MCPProvide read off CandyView (the identity/graph half — these
// carry the LIST-subcommand detail, not the build model); Artifact/Capabilities/
// RequiresCapabilities/Engine/Libvirt/Secret/PortSpecs read off CandyModel (the build half,
// widened with the same 5 fields added to #CandyModel alongside this fill).
func (a *specCandyAdapter) Alias() []AliasYAML               { return a.v.Aliases }
func (a *specCandyAdapter) HasAliases() bool                 { return len(a.v.Aliases) > 0 }
func (a *specCandyAdapter) Volume() []VolumeYAML             { return a.v.Volumes }
func (a *specCandyAdapter) HasVolumes() bool                 { return len(a.v.Volumes) > 0 }
func (a *specCandyAdapter) EnvProvides() map[string]string   { return a.v.EnvProvides }
func (a *specCandyAdapter) MCPProvide() []MCPServerYAML      { return a.v.MCPProvide }
func (a *specCandyAdapter) Artifact() []CandyArtifact        { return a.m.Artifact }
func (a *specCandyAdapter) Capabilities() *CandyCapabilities { return a.m.Capability }
func (a *specCandyAdapter) RequiresCapabilities() []string   { return a.m.RequiresCapability }
func (a *specCandyAdapter) Engine() string                   { return a.m.Engine }
func (a *specCandyAdapter) Libvirt() []string                { return a.m.Libvirt }
func (a *specCandyAdapter) HasLibvirt() bool                 { return len(a.m.Libvirt) > 0 }
func (a *specCandyAdapter) Secret() []SecretYAML             { return a.m.Secret }
func (a *specCandyAdapter) PortSpecs() []PortSpec            { return a.m.Port }
func (a *specCandyAdapter) HasPorts() bool                   { return len(a.m.Port) > 0 }
func (a *specCandyAdapter) HasEnvAccepts() bool              { return len(a.m.EnvAccept) > 0 }
func (a *specCandyAdapter) HasEnvProvides() bool             { return len(a.v.EnvProvides) > 0 }
func (a *specCandyAdapter) HasEnvRequires() bool             { return len(a.m.EnvRequire) > 0 }
func (a *specCandyAdapter) HasMCPAccepts() bool              { return len(a.m.MCPAccept) > 0 }
func (a *specCandyAdapter) HasMCPProvides() bool             { return len(a.v.MCPProvide) > 0 }
func (a *specCandyAdapter) HasMCPRequires() bool             { return len(a.m.MCPRequire) > 0 }
func (a *specCandyAdapter) HasSecretAccepts() bool           { return len(a.m.SecretAccept) > 0 }
func (a *specCandyAdapter) HasSecretRequires() bool          { return len(a.m.SecretRequire) > 0 }

// Plugin declaration (W9): the candy's OWN `plugin:` block, read off the identity/graph view.
func (a *specCandyAdapter) IsPluginCandy() bool          { return a.v.IsPlugin }
func (a *specCandyAdapter) GetPluginSource() string      { return a.v.PluginSource }
func (a *specCandyAdapter) GetPluginProviders() []string { return a.v.PluginProviders }

// LocalPkgFormats returns the sorted list of package formats with a bundled local source
// (localpkg: map keys) — the envelope carries the same map CollectLocalPkg needs.
func (a *specCandyAdapter) LocalPkgFormats() []string {
	if len(a.m.LocalPkg) == 0 {
		return nil
	}
	out := make([]string, 0, len(a.m.LocalPkg))
	for f := range a.m.LocalPkg {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Port returns the candy's raw authored port list ("8080"/"http:8080" strings) — the
// pre-normalization sibling of PortSpecs(), derived from it since the envelope carries only
// the normalized PortSpec form. Error return kept for interface/API-stability parity with the
// charly *Candy.Port() signature (never non-nil here — the envelope has no I/O to fail on).
func (a *specCandyAdapter) Port() ([]string, error) {
	if len(a.m.Port) == 0 {
		return nil, nil
	}
	out := make([]string, len(a.m.Port))
	for i, p := range a.m.Port {
		if p.Protocol != "" {
			out[i] = p.Protocol + ":" + strconv.Itoa(p.Port)
		} else {
			out[i] = strconv.Itoa(p.Port)
		}
	}
	return out, nil
}

var _ CandyModel = (*specCandyAdapter)(nil)
