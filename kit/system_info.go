package kit

// system_info.go — the `system:` section populator for the per-host charly.yml.
//
// The per-host charly.yml (~/.config/charly/charly.yml) is the SINGLE home for
// local system state — deployments (`deploy:`), install records (`ledger:`),
// local system info (`system:`), and cache status (`cache:`). This file
// populates the `system:` section (spec#66 schema/system.cue #SystemInfo) with
// the host identity snapshot: hostname, distro, kernel, arch, GPU,
// virtualization, podman. A command can then answer "what is this host?" from
// the unified local config instead of re-probing the host on every invocation.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/opencharly/spec/hostenv"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// detectSystemInfo probes the host and returns the identity snapshot. Every
// probe is best-effort: an unreadable /etc/os-release, a missing podman, or an
// undetectable GPU leaves that field empty rather than failing the whole
// snapshot.
func detectSystemInfo() spec.SystemInfo {
	info := spec.SystemInfo{
		Arch:      runtime.GOARCH,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}
	if hd, err := hostenv.DetectHostDistro(); err == nil {
		info.DistroID = hd.ID
		info.DistroVersion = hd.VersionID
	}
	if k := runFirstOutput("uname", "-r"); k != "" {
		info.Kernel = k
	}
	if a := runFirstOutput("uname", "-m"); a != "" {
		info.Arch = a
	}
	if v := runFirstOutput("systemd-detect-virt"); v != "" {
		info.Virtualization = v
	}
	if p := runFirstOutput("podman", "--version"); p != "" {
		info.Podman = p
	}
	info.GPU = detectGPU()
	return info
}

// runFirstOutput runs a command and returns its trimmed stdout (first line), or
// "" on any error/absence.
func runFirstOutput(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return line
}

// detectGPU probes the primary GPU: the first /dev/dri/renderD* device (the
// kernel's DRM render node — present for any GPU with a driver) or the first
// lspci VGA/3D controller line. Best-effort; "" when undetectable.
func detectGPU() string {
	// The DRM render node is the most reliable cross-vendor signal (Intel, AMD,
	// NVIDIA all expose /dev/dri/renderD* when a driver is loaded).
	if matches, err := filepath.Glob("/dev/dri/renderD*"); err == nil && len(matches) > 0 {
		return "drm:" + filepath.Base(matches[0])
	}
	// Fall back to lspci's VGA/3D controller line.
	if out, err := exec.Command("lspci").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "VGA compatible") || strings.Contains(line, "3D controller") {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

// PopulateSystemInfo detects the host identity and writes it into the `system:`
// section of the per-host charly.yml, preserving every other key (deploy:,
// provides:, cache:, ledger:). It is the "refresh the local system snapshot"
// operation — called by `charly status` (and available to any command that
// needs the host identity persisted).
func PopulateSystemInfo() error {
	path, err := spec.DefaultDeployConfigPath()
	if err != nil {
		return err
	}
	info := detectSystemInfo()
	return writeSystemInfo(path, info)
}

// writeSystemInfo persists the `system:` section into the per-host charly.yml
// under the advisory lock (best-effort). It reads the CURRENT file, updates
// only the `system:` key (preserving every other key), and writes back
// atomically (tempfile + rename).
func writeSystemInfo(path string, info spec.SystemInfo) error {
	unlock, err := AcquireFileLock(path+".lock", true)
	if err != nil {
		return err
	}
	defer unlock()

	data, err := os.ReadFile(path)
	var doc yaml.Node
	if err == nil {
		if yaml.Unmarshal(data, &doc) != nil {
			return nil // corrupt file — never clobber it
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
			{Kind: yaml.MappingNode, Tag: "!!map"},
		}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	if !hasMappingKey(root, "version") {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "version"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: spec.SchemaVersion},
		)
	}
	body, err := yaml.Marshal(info)
	if err != nil {
		return err
	}
	var val yaml.Node
	if err := yaml.Unmarshal(body, &val); err != nil || len(val.Content) == 0 {
		return err
	}
	SetMappingKey(root, "system", val.Content[0])

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".charly-system-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
