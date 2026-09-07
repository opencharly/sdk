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
func ProjectDeployConfig(uf *spec.UnifiedFile) *DeployConfig {
	if uf == nil {
		return nil
	}
	sidecars := uf.PluginKinds["sidecar"] // opaque bodies; candy/plugin-sidecar resolves them
	if len(uf.Deploy) == 0 && uf.Provides == nil && len(sidecars) == 0 {
		return nil
	}
	return &DeployConfig{
		Provides: uf.Provides,
		Deploy:   uf.Deploy,
		Sidecar:  sidecars,
	}
}
