package kit

import (
	"fmt"
	"os"
	"path/filepath"
)

// quadlet_paths.go — the on-disk quadlet/systemd path + filename helpers (K4:
// relocated from charly/quadlet.go — pure string/path formatting + a host
// existence probe, no project-loader dependency).
//
// CURRENT STATE (corrected 2026-07-20): as of this commit, charly main STILL carries the
// original self-contained charly/quadlet.go — the companion DEPLOY-wave charly PR deletes it
// and repoints every caller (bundle_add_cmd.go, bundle_cmd.go, preempt.go, config_image.go,
// commands.go, and the pod-lifecycle files) to kit.QuadletDir/kit.QuadletFilenameInstance/etc.
// directly (K3 ZERO-ALIASES — no alias file), tracked there, not yet true on main today.

// QuadletDir returns the user-level quadlet directory.
func QuadletDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config", "containers", "systemd"), nil
}

// SystemdUserDir returns the user-level systemd unit directory (~/.config/systemd/user/).
func SystemdUserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// QuadletFilename returns the quadlet filename for an image.
func QuadletFilename(boxName string) string {
	return ContainerName(boxName) + ".container"
}

// QuadletFilenameInstance returns the quadlet filename for an image with optional instance.
func QuadletFilenameInstance(boxName, instance string) string {
	return ContainerNameInstance(boxName, instance) + ".container"
}

// ServiceName returns the systemd service name for an image.
func ServiceName(boxName string) string {
	return ContainerName(boxName) + ".service"
}

// ServiceNameInstance returns the systemd service name for an image with optional instance.
func ServiceNameInstance(boxName, instance string) string {
	return ContainerNameInstance(boxName, instance) + ".service"
}

// QuadletExists checks whether a .container file exists for the given image.
func QuadletExists(boxName string) (bool, error) {
	return QuadletExistsInstance(boxName, "")
}

// QuadletExistsInstance checks whether a .container file exists for the given image/instance.
func QuadletExistsInstance(boxName, instance string) (bool, error) {
	qdir, err := QuadletDir()
	if err != nil {
		return false, err
	}
	qpath := filepath.Join(qdir, QuadletFilenameInstance(boxName, instance))
	_, err = os.Stat(qpath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// PodQuadletFilename returns the quadlet filename for a pod (K4: relocated from
// charly/quadlet_pod.go — pure string formatting, no project-loader dependency).
func PodQuadletFilename(boxName string) string {
	return PodName(boxName) + ".pod"
}

// PodQuadletFilenameInstance returns the quadlet filename for a pod with optional instance.
func PodQuadletFilenameInstance(boxName, instance string) string {
	return PodNameInstance(boxName, instance) + ".pod"
}

// SidecarQuadletFilename returns the quadlet filename for a sidecar container.
func SidecarQuadletFilename(boxName, sidecarName string) string {
	return SidecarContainerName(boxName, sidecarName) + ".container"
}

// SidecarQuadletFilenameInstance returns the quadlet filename for a sidecar with optional instance.
func SidecarQuadletFilenameInstance(boxName, instance, sidecarName string) string {
	return SidecarContainerNameInstance(boxName, instance, sidecarName) + ".container"
}
