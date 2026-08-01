package kit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/spec/spec"
)

// sidecar_naming.go — the host-FS sidecar helper (SidecarConfigDir) STAYS here; the pure
// sidecar/pod container-NAMING funcs relocated to spec/spec/sidecar_naming.go (#55 value
// extraction) and are re-exported below so existing kit.PodName / kit.SidecarContainerName…
// call sites (candy/plugin-deploy-pod, charly core config_image.go / quadlet_paths.go) are
// untouched. New consumers should reference spec.* directly.

var (
	PodName                      = spec.PodName
	PodNameInstance              = spec.PodNameInstance
	SidecarContainerName         = spec.SidecarContainerName
	SidecarContainerNameInstance = spec.SidecarContainerNameInstance
)

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
