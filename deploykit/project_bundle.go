package deploykit

import (
	"github.com/opencharly/spec/spec"
)

// project_bundle.go — the ONE loader-result method that could NOT travel to spec with UnifiedFile:
// spec is types-only and must never import a mechanism kit, but this projection returns a
// *deploykit.BundleConfig. So it is a deploykit FREE FUNCTION (was uf.ProjectBundleConfig() in
// loaderkit; #55 Phase B). Every former caller changes uf.ProjectBundleConfig() →
// deploykit.ProjectBundleConfig(uf).

// ProjectBundleConfig returns the *BundleConfig equivalent (the deployments: section of the authored
// file, independent of any per-machine ~/.config/charly/charly.yml, which remains loaded separately
// by LoadBundleConfig). nil when the file carries no deploy/provides/sidecar content.
func ProjectBundleConfig(uf *spec.UnifiedFile) *BundleConfig {
	if uf == nil {
		return nil
	}
	sidecars := uf.PluginKinds["sidecar"] // opaque bodies; candy/plugin-sidecar resolves them
	if len(uf.Bundle) == 0 && uf.Provides == nil && len(sidecars) == 0 {
		return nil
	}
	return &BundleConfig{
		Provides: uf.Provides,
		Bundle:   uf.Bundle,
		Sidecar:  sidecars,
	}
}
