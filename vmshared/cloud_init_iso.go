package vmshared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/opencharly/sdk/proclifecycle"
)

// WriteSeedISO builds a NoCloud cidata ISO at outPath. Takes the three
// rendered strings from RenderCloudInit (user-data, meta-data, and
// optional network-config) and shells out to xorriso to pack them into
// an ISO9660+Joliet+RockRidge image labeled cloudInitVolumeID.
//
// The guest's cloud-init scans for a filesystem labeled "CIDATA" or
// "cidata" (it tries both cases) on first boot. Files inside:
//
//	user-data       — the #cloud-config YAML (required)
//	meta-data       — instance-id + hostname (required, can be empty)
//	network-config  — v2 network schema (optional)
//
// Returns an error if xorriso isn't installed or the ISO write fails.
// charly doctor checks for xorriso and suggests the install package.
func WriteSeedISO(outPath, userData, metaData, networkConfig string) error {
	if userData == "" {
		return fmt.Errorf("WriteSeedISO: user-data is empty")
	}

	// Stage files in a tempdir. xorriso requires real paths on disk.
	tmpDir, err := os.MkdirTemp("", "charly-cidata-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	proclifecycle.RegisterTempCleanup(tmpDir)
	defer func() { _ = os.RemoveAll(tmpDir); proclifecycle.UnregisterTempCleanup(tmpDir) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(userData), 0o644); err != nil {
		return fmt.Errorf("writing user-data: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaData), 0o644); err != nil {
		return fmt.Errorf("writing meta-data: %w", err)
	}

	var files []string
	files = append(files, filepath.Join(tmpDir, "user-data"))
	files = append(files, filepath.Join(tmpDir, "meta-data"))

	if networkConfig != "" {
		if err := os.WriteFile(filepath.Join(tmpDir, "network-config"), []byte(networkConfig), 0o644); err != nil {
			return fmt.Errorf("writing network-config: %w", err)
		}
		files = append(files, filepath.Join(tmpDir, "network-config"))
	}

	// Ensure parent dir of outPath exists.
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	// Pick ISO builder: xorriso -as mkisofs is preferred (available on
	// every major distro via the xorriso/libisoburn package). Fallback
	// to genisoimage when xorriso isn't on PATH.
	builder := resolveISOBuilder()
	if builder.Bin == "" {
		return fmt.Errorf("no ISO builder found on PATH; install xorriso (preferred) or genisoimage/mkisofs")
	}

	args := builder.Args(outPath, files)

	cmd := exec.Command(builder.Bin, args...)
	cmd.Stdout = nil // xorriso prints voluminous progress by default
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", builder.Bin, err)
	}
	return nil
}

// cloudInitVolumeID is the ISO 9660 volume identifier cloud-init's NoCloud
// datasource looks for. It MUST be uppercase: ISO 9660 / ECMA 119 d-characters
// are A-Z 0-9 _ only, so a lowercase label makes xorriso warn on every single
// VM boot ("-volid text does not comply to ISO 9660 / ECMA 119 rules").
//
// Uppercase is safe: NoCloud searches BOTH cases — DataSourceNoCloud._get_devices
// does find_devs_with("LABEL=" + label.upper()) then ...label.lower(), with label
// defaulting to "cidata". Verified by reading the datasource inside a live guest,
// not from memory.
const cloudInitVolumeID = "CIDATA"

// isoBuilder is a chosen ISO-builder binary + its argv-construction
// strategy. xorriso and genisoimage/mkisofs accept compatible flags
// via xorriso's "-as mkisofs" mode, but call separately to keep argv
// explicit.
type isoBuilder struct {
	Bin  string
	Args func(outPath string, files []string) []string
}

func resolveISOBuilder() isoBuilder {
	if bin, err := exec.LookPath("xorriso"); err == nil {
		return isoBuilder{
			Bin: bin,
			Args: func(out string, files []string) []string {
				args := make([]string, 0, 8+len(files))
				args = append(args, "-as", "mkisofs", "-volid", cloudInitVolumeID, "-joliet", "-rock", "-output", out)
				return append(args, files...)
			},
		}
	}
	if bin, err := exec.LookPath("genisoimage"); err == nil {
		return isoBuilder{
			Bin: bin,
			Args: func(out string, files []string) []string {
				args := make([]string, 0, 6+len(files))
				args = append(args, "-volid", cloudInitVolumeID, "-joliet", "-rock", "-output", out)
				return append(args, files...)
			},
		}
	}
	if bin, err := exec.LookPath("mkisofs"); err == nil {
		return isoBuilder{
			Bin: bin,
			Args: func(out string, files []string) []string {
				args := make([]string, 0, 6+len(files))
				args = append(args, "-volid", cloudInitVolumeID, "-joliet", "-rock", "-output", out)
				return append(args, files...)
			},
		}
	}
	return isoBuilder{}
}

