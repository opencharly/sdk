package deploykit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/spec/spec"
	"github.com/opencharly/spec/storeroot"
)

// quadlet_paths.go — the on-disk quadlet/systemd PATH helpers + host existence probes
// (QuadletDir / SystemdUserDir / QuadletExists[Instance] — os.UserHomeDir + os.Stat). The pure
// quadlet/service FILENAME funcs are the single-source spec/spec/quadlet_names.go (#55 value
// extraction — one copy on spec, R3); re-exported below so
// existing deploykit.QuadletFilename / deploykit.ServiceName… call sites are untouched. Distinct
// from this package's OWN quadlet.go (the config-WRITE MECHANISM — GenerateQuadlet + its
// emitters). New consumers should reference spec.* directly.

var (
	QuadletFilename         = spec.QuadletFilename
	QuadletFilenameInstance = spec.QuadletFilenameInstance
	ServiceName             = spec.ServiceName
	ServiceNameInstance     = spec.ServiceNameInstance
)

// QuadletDir returns the user-level quadlet directory under the XDG config base.
func QuadletDir() (string, error) {
	base, err := storeroot.Base(storeroot.RootConfig)
	if err != nil {
		return "", fmt.Errorf("determining config base: %w", err)
	}
	return filepath.Join(base, "containers", "systemd"), nil
}

// SystemdUserDir returns the user-level systemd unit directory under the XDG config base.
func SystemdUserDir() (string, error) {
	base, err := storeroot.Base(storeroot.RootConfig)
	if err != nil {
		return "", fmt.Errorf("determining config base: %w", err)
	}
	return filepath.Join(base, "systemd", "user"), nil
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
