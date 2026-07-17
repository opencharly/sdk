package kit

import (
	"fmt"
	"os"
	"path/filepath"
)

// sidecar_naming.go — the pure sidecar/pod container-naming helpers (K4: relocated from
// charly/sidecar.go — genuinely pure string composition over ContainerName/ContainerNameInstance,
// no project-loader dependency). Consumed directly by candy/plugin-deploy-pod and by charly core's
// remaining callers (config_image.go, quadlet.go), which now import kit directly (K3 ZERO-ALIASES
// — no alias file).

// PodName returns the container name for a pod's primary container.
func PodName(boxName string) string {
	return ContainerName(boxName)
}

// PodNameInstance returns the container name for a pod's primary container, instance-aware.
func PodNameInstance(boxName, instance string) string {
	return ContainerNameInstance(boxName, instance)
}

// SidecarContainerName returns the container name for a named sidecar.
func SidecarContainerName(boxName, sidecarName string) string {
	return ContainerName(boxName) + "-" + sidecarName
}

// SidecarContainerNameInstance returns the container name for a named sidecar, instance-aware.
func SidecarContainerNameInstance(boxName, instance, sidecarName string) string {
	return ContainerNameInstance(boxName, instance) + "-" + sidecarName
}

// SidecarConfigDir returns the per-user directory where sidecar companion
// config files live (e.g. charly-foo-tailscale-serve.json), used by the
// `charly config remove` sidecar-config sweep.
func SidecarConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determining config directory: %w", err)
	}
	return filepath.Join(configDir, "charly", "sidecar"), nil
}
