package deploykit

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// deploy_file.go — the ZERO-dependency pure pieces of the deploy STATE model
// folded out of charly/deploy.go to sdk/deploykit (P13/C15), so a plugin (and any
// SDK consumer) reads/mutates a BundleConfig without importing charly core. Only
// the genuinely portable functions live here: LoadDeployFile is a plain file →
// BundleConfig unmarshal, and RemoveBoxDeploy is a plain map delete. The rest of
// the state model (LoadBundleConfig, SaveBundleConfig, saveDeployState,
// cleanDeployEntry, MergeDeployOntoMetadata, ExportAllBox) STAYS core — it is
// coupled to core Mechanisms the SDK cannot import: the unified LOADER
// (LoadUnified — validation + migration), the process-shared deploy-config FLOCK,
// the legacy→node-form migrate transform, and the runtime Config graph. So the
// per-host ledger is read/written by the compiled-in command plugins via the
// config-resolve / config-persist host seams (the plugin-vm precedent), while
// these two pure helpers move to the SDK library.

// LoadDeployFile reads a charly.yml from an arbitrary path into a BundleConfig.
func LoadDeployFile(path string) (*BundleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var dc BundleConfig
	if err := yaml.Unmarshal(data, &dc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &dc, nil
}

// RemoveBoxDeploy removes an image's entry from a deploy config.
func RemoveBoxDeploy(dc *BundleConfig, boxName string) {
	if dc != nil && dc.Bundle != nil {
		delete(dc.Bundle, boxName)
	}
}
