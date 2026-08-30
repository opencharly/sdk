package vmshared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencharly/spec/proc"
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
//
// This is a thin convenience over WriteLabeledISO: cloud-init's NoCloud datasource is one
// answers-volume format among several, and an unattended distro installer (archinstall,
// kickstart, preseed, autoinstall) wants the same ISO writer with a different label and a
// different file set. There is exactly ONE ISO writer here, and this is its NoCloud face.
func WriteSeedISO(outPath, userData, metaData, networkConfig string) error {
	if userData == "" {
		return fmt.Errorf("WriteSeedISO: user-data is empty")
	}
	files := map[string]string{"user-data": userData, "meta-data": metaData}
	if networkConfig != "" {
		files["network-config"] = networkConfig
	}
	return WriteLabeledISO(outPath, cloudInitVolumeID, files)
}

// WriteLabeledISO packs files into an ISO9660+Joliet+RockRidge image at outPath, labelled
// volumeID. The map is path-inside-the-volume -> content; keys must be plain names or
// relative paths, and content is written verbatim.
//
// The LABEL is what an unattended installer discovers the volume by — "CIDATA" for
// cloud-init NoCloud, "OEMDRV" for Anaconda kickstart — so it is a parameter rather than a
// constant. Case is preserved exactly as given: xorriso's -volid writes what it is handed,
// verified both ways, which is why a distro's #DistroInstaller.volume_id can be matched
// case-sensitively without needing a vfat fallback.
func WriteLabeledISO(outPath, volumeID string, files map[string]string) error {
	if volumeID == "" {
		return fmt.Errorf("WriteLabeledISO: volume ID is empty")
	}
	if len(files) == 0 {
		return fmt.Errorf("WriteLabeledISO: no files to write")
	}

	// Stage files in a tempdir. xorriso requires real paths on disk.
	// Short-lived (stage, xorriso, remove), but held on the same path as every other swept
	// namespace: a loaded host can stretch an ISO build past the sweep's floor, and the root's
	// mtime is frozen the moment the staged files land inside it.
	tmpDir, releaseTmpDir, err := proc.MkdirTempHeld("", "charly-cidata-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer releaseTmpDir()
	defer func() { _ = os.RemoveAll(tmpDir); proc.UnregisterTempCleanup(tmpDir) }()

	// Sorted, so the emitted ISO is byte-reproducible for the same input map. Go map
	// iteration order is randomised, and an ISO whose file order changes between runs
	// would defeat content-addressed caching downstream.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	staged := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return fmt.Errorf("WriteLabeledISO: %q is not a plain relative path inside the volume", name)
		}
		dst := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(name), err)
		}
		if err := os.WriteFile(dst, []byte(files[name]), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		staged = append(staged, dst)
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

	args := builder.Args(outPath, volumeID, staged)

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
	Args func(outPath, volumeID string, files []string) []string
}

func resolveISOBuilder() isoBuilder {
	if bin, err := exec.LookPath("xorriso"); err == nil {
		return isoBuilder{
			Bin: bin,
			Args: func(out, volumeID string, files []string) []string {
				args := make([]string, 0, 8+len(files))
				args = append(args, "-as", "mkisofs", "-volid", volumeID, "-joliet", "-rock", "-output", out)
				return append(args, files...)
			},
		}
	}
	if bin, err := exec.LookPath("genisoimage"); err == nil {
		return isoBuilder{
			Bin: bin,
			Args: func(out, volumeID string, files []string) []string {
				args := make([]string, 0, 6+len(files))
				args = append(args, "-volid", volumeID, "-joliet", "-rock", "-output", out)
				return append(args, files...)
			},
		}
	}
	if bin, err := exec.LookPath("mkisofs"); err == nil {
		return isoBuilder{
			Bin: bin,
			Args: func(out, volumeID string, files []string) []string {
				args := make([]string, 0, 6+len(files))
				args = append(args, "-volid", volumeID, "-joliet", "-rock", "-output", out)
				return append(args, files...)
			},
		}
	}
	return isoBuilder{}
}
