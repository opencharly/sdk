package kit

import (
	"fmt"
	"os"
	"path/filepath"
)

// direct_deploy.go — the direct-mode deploy marker probe (K4 lane B: relocated from
// charly/config_image.go — a genuinely pure host-filesystem mechanism). Shared between charly
// core's remaining callers (config_image.go) and candy/plugin-deploy-pod
// (pod_lifecycle_resolve.go's quadlet-mode move — resolvePodStartQuadlet's direct-deploy marker
// check) via the kit_aliases.go passthrough.

// DirectDeployMarkerDir returns ~/.config/charly/direct/, the registry
// directory for direct-mode deploys (the equivalent of
// ~/.config/containers/systemd/ for quadlet deploys).
func DirectDeployMarkerDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home: %w", err)
	}
	return filepath.Join(home, ".config", "charly", "direct"), nil
}

// DirectDeployMarkerPath returns the marker JSON path for a deploy.
func DirectDeployMarkerPath(box, instance string) (string, error) {
	dir, err := DirectDeployMarkerDir()
	if err != nil {
		return "", err
	}
	name := ContainerNameInstance(box, instance)
	return filepath.Join(dir, name+".json"), nil
}

// IsDirectDeploy reports whether the named deploy was created in
// direct mode (i.e. has a marker file). Used by lifecycle commands.
func IsDirectDeploy(box, instance string) bool {
	path, err := DirectDeployMarkerPath(box, instance)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
