package vmshared

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/opencharly/spec/hostenv"
)

// vm_helpers.go — pure VM helper functions shared by charly core (the `charly vm` command path +
// host probes) AND the out-of-process candy/plugin-vm (its verbs/deploy). They touch only the
// filesystem, /proc, the OS process table, and string/XML/JSON parsing — no go-libvirt, no govmm —
// so a single copy lives here (FU-10 consolidated them from the former core↔plugin duplication).
// Each module aliases the lowercase name it used before (e.g. var resolveVmRam = vmshared.ResolveVmRam,
// in its vmshared_aliases.go), so existing call sites are untouched.

// ResolveVmRam picks the spec-declared RAM or falls back to "4G".
func ResolveVmRam(spec *VmSpec) string {
	if spec.Ram != "" {
		return spec.Ram
	}
	return "4G"
}

// ResolveVmCpus picks the spec-declared CPU count or falls back to 2.
func ResolveVmCpus(spec *VmSpec) int {
	if spec.Cpus > 0 {
		return spec.Cpus
	}
	return 2
}

// DetectRuntimeHostVendor reads /proc/cpuinfo to identify the host CPU vendor
// (GenuineIntel | AuthenticAMD | ""). Used by RenderDomain / RenderQemuArgv to
// auto-append the correct nested-virt feature (vmx vs svm).
func DetectRuntimeHostVendor() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "vendor_id") {
			if idx := strings.Index(line, ":"); idx > 0 {
				return strings.TrimSpace(line[idx+1:])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: /proc/cpuinfo scan error: %v\n", err)
	}
	return ""
}

// QemuSystemBinary returns the architecture-appropriate QEMU binary name.
func QemuSystemBinary() string {
	switch runtime.GOARCH {
	case "arm64":
		return "qemu-system-aarch64"
	default:
		return "qemu-system-x86_64"
	}
}

// VmImageDirEnv overrides the VM image root (the "vm.image_dir" setting's env
// counterpart, mirroring CHARLY_VM_STATE_DIR). Relative values resolve against
// the working project; absolute values pin it globally.
const VmImageDirEnv = "CHARLY_VM_IMAGE_DIR"

// VmDiskDir returns the per-VM directory holding a built disk image (disk.qcow2)
// and, for cloud_image/bootstrap/clone sources, its NoCloud seed.iso. The path is
// namespaced by the DISK SOURCE (the kind:vm ENTITY), so it is the shared
// read-only BASE every per-deploy overlay backs onto — building or creating one
// entity never reuses a SIBLING entity's disk or — critically — its stale
// seed.iso, whose embedded SSH key would mismatch this VM's own id_ed25519 and
// silently break the deploy's authentication.
//
// The root is CONFIGURABLE (vm.image_dir setting / CHARLY_VM_IMAGE_DIR env,
// default "image") — never a hardcoded literal (the former "output" default was
// a footgun: it read as disposable build scratch, so it got deleted as such
// while live VMs' backing chains still pointed into it). The (string, error)
// shape mirrors VmStateRoot: a root-resolution failure is PROPAGATED, never
// silently substituted with a different root (which would point a live VM at a
// path its backing chain does not use — the exact failure mode this replaces).
func VmDiskDir(vmName string) (string, error) {
	root, err := VmDiskRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, vmName), nil
}

// VmDiskRoot resolves the root directory for built VM disk images. Precedence:
// the CHARLY_VM_IMAGE_DIR env var > the `vm.image_dir` config setting > the
// built-in default "image". A relative result is intentionally returned as-is:
// call sites resolve it against the working project (the disk is a build product
// of the project's own VMs), exactly like the former hardcoded relative path.
//
// A MISSING config is not an error (LoadRuntimeConfig returns a zero config, nil)
// — the "image" default applies. A non-nil error means the config file exists but
// could not be read; that is PROPAGATED (never masked by substituting a root).
func VmDiskRoot() (string, error) {
	if raw := strings.TrimSpace(os.Getenv(VmImageDirEnv)); raw != "" {
		return raw, nil
	}
	cfg, err := hostenv.LoadRuntimeConfig()
	if err != nil {
		return "", fmt.Errorf("resolving the VM disk-image root: reading the runtime config failed: %w", err)
	}
	if v := strings.TrimSpace(cfg.Vm.ImageDir); v != "" {
		return v, nil
	}
	return "image", nil
}

// KillQemuByPID force-kills a direct-QEMU VM by the PID recorded in its state dir (the last-resort
// path when QMP graceful/force shutdown is unavailable). Pure OS process kill — no govmm.
func KillQemuByPID(stateDir string) {
	proc, _, ok := qemuProcFromStateDir(stateDir)
	if !ok {
		return
	}
	_ = proc.Kill()
}

// LibvirtSessionSocket returns the path to the user's libvirt session socket. Modern libvirt (≥ 8.0)
// uses per-driver modular daemons (virtqemud-sock); legacy libvirt (< 8.0) uses the monolithic
// libvirt-sock. Probe the modular socket first (every current distro), then the monolithic one.
// Returns "" when neither exists — never a path that was not found.
func LibvirtSessionSocket() string {
	picked, _ := LibvirtSessionSocketWithProbes()
	return picked
}

func LibvirtSessionSocketWithProbes() (picked string, probed []string) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	libvirtDir := filepath.Join(dir, "libvirt")

	// Probe order: modular (virtqemud) first — standard on libvirt ≥ 8.0 — then legacy monolithic.
	modular := filepath.Join(libvirtDir, "virtqemud-sock")
	legacy := filepath.Join(libvirtDir, "libvirt-sock")
	probed = []string{modular, legacy}

	if _, err := os.Stat(modular); err == nil {
		return modular, probed
	}
	if _, err := os.Stat(legacy); err == nil {
		return legacy, probed
	}
	// Neither socket exists: return "" rather than a path that is not there. The previous
	// `return legacy` handed callers a nonexistent path as though it had been found, which
	// made status_vm's own `if sock == ""` guard DEAD CODE and turned "libvirt is not
	// running" into a stat failure on a monolithic socket the host may never have used.
	// `probed` still carries both candidates for the diagnostic trail.
	return "", probed
}

// WriteJSON encodes v as indented JSON to w (the `--json` output helper; the
// `charly vm snapshot list --json` path uses it).
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// libvirtDeviceElements lists element names that belong inside <devices> in libvirt domain XML.
var libvirtDeviceElements = map[string]bool{
	"channel":    true,
	"disk":       true,
	"controller": true,
	"filesystem": true,
	"hostdev":    true,
	"interface":  true,
	"serial":     true,
	"console":    true,
	"input":      true,
	"graphics":   true,
	"video":      true,
	"sound":      true,
	"audio":      true,
	"watchdog":   true,
	"memballoon": true,
	"rng":        true,
	"tpm":        true,
	"redirdev":   true,
	"smartcard":  true,
	"hub":        true,
	"panic":      true,
	"shmem":      true,
	"memory":     true,
	"iommu":      true,
	"vsock":      true,
}

// IsDeviceElement returns true if the XML snippet's root element belongs inside <devices>.
func IsDeviceElement(snippet string) bool {
	decoder := xml.NewDecoder(strings.NewReader(snippet))
	for {
		tok, err := decoder.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return libvirtDeviceElements[se.Name.Local]
		}
	}
}
