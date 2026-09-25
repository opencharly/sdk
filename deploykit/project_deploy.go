package deploykit

import (
	"github.com/opencharly/spec/spec"
)

// project_deploy.go — the ONE loader-result method that could NOT travel to spec with UnifiedFile:
// spec is types-only and must never import a mechanism kit, but this projection returns a
// *deploykit.DeployConfig. So it is a deploykit FREE FUNCTION (was uf.ProjectDeployConfig() in
// loaderkit; #55 Phase B). Every former caller changes uf.ProjectDeployConfig() →
// deploykit.ProjectDeployConfig(uf).

// ProjectDeployConfig returns the *DeployConfig equivalent (the deployments: section of the authored
// file, independent of any per-machine ~/.config/charly/charly.yml, which remains loaded separately
// by LoadDeployConfig). nil when the file carries no deploy/provides/sidecar content.
//
// The Deploy map is NAMESPACE-QUALIFIED (spec.UnifiedFile.Deploys): local deploys under their bare
// name plus every imported namespace's as `ns.name`. This is what lets a merged-root consumer
// resolve a namespaced deploy (`charly deploy add charly.check-agentteams-vm`) — the raw
// uf.Deploy map is root-scope only, so the walk found no entry and defaulted the target to "pod".
// Qualified keys never collide with bare local names, so this is additive.
func ProjectDeployConfig(uf *spec.UnifiedFile) *DeployConfig {
	if uf == nil {
		return nil
	}
	sidecars := uf.PluginKinds["sidecar"] // opaque bodies; candy/plugin-sidecar resolves them
	deploys := uf.Deploys()
	if len(deploys) == 0 && uf.Provides == nil && len(sidecars) == 0 {
		return nil
	}
	return &DeployConfig{
		Provides: uf.Provides,
		Deploy:   deploys,
		Sidecar:  sidecars,
	}
}
