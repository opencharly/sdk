package deploykit

import (
	"github.com/opencharly/spec/spec"
)

// project_fleet.go — the ONE loader-result method that could NOT travel to spec with UnifiedFile:
// spec is types-only and must never import a mechanism kit, but this projection returns a
// *deploykit.FleetConfig. So it is a deploykit FREE FUNCTION (was uf.ProjectFleetConfig() in
// loaderkit; #55 Phase B). Every former caller changes uf.ProjectFleetConfig() →
// deploykit.ProjectFleetConfig(uf).

// ProjectFleetConfig returns the *FleetConfig equivalent (the deployments: section of the authored
// file, independent of any per-machine ~/.config/charly/charly.yml, which remains loaded separately
// by LoadFleetConfig). nil when the file carries no deploy/provides/sidecar content.
func ProjectFleetConfig(uf *spec.UnifiedFile) *FleetConfig {
	if uf == nil {
		return nil
	}
	sidecars := uf.PluginKinds["sidecar"] // opaque bodies; candy/plugin-sidecar resolves them
	if len(uf.Fleet) == 0 && uf.Provides == nil && len(sidecars) == 0 {
		return nil
	}
	return &FleetConfig{
		Provides: uf.Provides,
		Fleet:    uf.Fleet,
		Sidecar:  sidecars,
	}
}
