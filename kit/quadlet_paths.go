package kit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/spec/spec"
)

// quadlet_paths.go — the on-disk quadlet/systemd path helpers + host existence probes STAY
// here (QuadletDir / SystemdUserDir / QuadletExists[Instance] — os.UserHomeDir + os.Stat); the
// pure quadlet/service FILENAME funcs relocated to spec/spec/quadlet_names.go (#55 value
// extraction) and are re-exported below so existing kit.QuadletFilename / kit.ServiceName…
// call sites are untouched. The host-fs probes call the re-exported pure names. New consumers
// should reference spec.* directly.

var (
	ServiceName                    = spec.ServiceName
	ServiceNameInstance            = spec.ServiceNameInstance
	QuadletFilename                = spec.QuadletFilename
	QuadletFilenameInstance        = spec.QuadletFilenameInstance
	PodQuadletFilename             = spec.PodQuadletFilename
	PodQuadletFilenameInstance     = spec.PodQuadletFilenameInstance
	SidecarQuadletFilename         = spec.SidecarQuadletFilename
	SidecarQuadletFilenameInstance = spec.SidecarQuadletFilenameInstance
)

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
