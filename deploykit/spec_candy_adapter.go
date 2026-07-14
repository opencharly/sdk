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
func (a *specCandyAdapter) HasInit(string) bool {
	return len(a.m.ServiceFiles) > 0 || len(a.m.Service) > 0
}

func (a *specCandyAdapter) GetExternalBuilder() string { return a.m.ExternalBuilder }

// GetSubPathPrefix — the parent dir within the repo for a remote candy's COPY-source
// (RepoPath + SubPathPrefix + ref). Filled on the CandyView by the resolve projector (#67
// build-render move); the build-mode render (candyCopySource) reads it to reproduce remote
// COPY sources WITHOUT the live *Candy. (Was stubbed "" while build-mode was host-only.)
func (a *specCandyAdapter) GetSubPathPrefix() string { return a.v.SubPathPrefix }

var _ CandyModel = (*specCandyAdapter)(nil)
