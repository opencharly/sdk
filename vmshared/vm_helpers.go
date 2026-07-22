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
	"strconv"
	"strings"
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

// VmDiskDir returns the per-VM directory holding a built disk image (disk.qcow2) and, for
// cloud_image/bootstrap/clone sources, its NoCloud seed.iso. The path is namespaced by the DISK
// SOURCE (the kind:vm ENTITY), so it is the shared read-only BASE every per-deploy overlay backs
// onto — building or creating one entity never reuses a SIBLING entity's disk or — critically — its
// stale seed.iso, whose embedded SSH key would mismatch this VM's own id_ed25519 and silently break
// the deploy's authentication.
func VmDiskDir(vmName string) string {
	return filepath.Join("output", "qcow2", vmName)
}

// VmDomainIdentity normalizes a deploy/bundle name into the per-deploy VM DOMAIN IDENTITY — the
// token that keys the libvirt domain (charly-<identity>), the per-domain state dir, the managed
// ssh-config alias, and the ssh-port ledger entry (vm:<identity>). It is DISTINCT from the kind:vm
// ENTITY (the disk/spec source, resolved via the deploy's `from:` cross-ref): several distinct beds
// may share one entity, so keying the domain by the ENTITY collided them on one libvirt domain +
// one disk + one host SSH port. Keying by the DEPLOY NAME instead makes distinct beds collision-free
// by construction (each gets its own domain, per-deploy disk overlay, and auto-allocated port).
//
// The normalization strips a leading "vm:" prefix and flattens the instance ("/") and nested-path
// (".") separators to "-", so a bare bed name maps to itself (check-builder-vm → check-builder-vm),
// a bundle ref maps to its VM token (vm:arch → arch), and a direct `charly vm create <entity>` (whose
// domain identity IS the entity) is unchanged. Both the host preresolver and candy/plugin-deploy-vm
// call this on the SAME deploy name, so the domain they derive always agrees.
func VmDomainIdentity(deployName string) string {
	id := strings.TrimPrefix(deployName, "vm:")
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, ".", "-")
	return id
}

// KillQemuByPID force-kills a direct-QEMU VM by the PID recorded in its state dir (the last-resort
// path when QMP graceful/force shutdown is unavailable). Pure OS process kill — no govmm.
func KillQemuByPID(stateDir string) {
	pidFile := filepath.Join(stateDir, "qemu.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
}

// LibvirtSessionSocket returns the path to the user's libvirt session socket. Modern libvirt (≥ 8.0)
// uses per-driver modular daemons (virtqemud-sock); legacy libvirt (< 8.0) uses the monolithic
// libvirt-sock. Probe the modular socket first (every current distro), fall back to legacy.
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
	return legacy, probed
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
