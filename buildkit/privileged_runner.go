package buildkit

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/spec"
)

// privileged_runner.go — the shared privileged-container exec primitive for both the
// OCI-image bootstrap path (candy/plugin-build's runPrivilegedBootstrap) and the VM-disk
// build engine (candy/plugin-vm). Formerly duplicated (charly/privileged_runner.go +
// candy/plugin-vm/vm_privileged_runner.go, byte-for-byte identical bodies) — unified here
// per R3, since every dependency (kit.ResolveRuntime/EngineBinary/TransferToRootful,
// proc.RegisterTempCleanup/UnregisterTempCleanup, spec.Phase/Venue enums,
// TemplateFuncs/BuilderPhaseTemplate) is already sdk-importable and neither caller needs
// anything core-only.

// PrivilegedRun describes a single privileged-container invocation: a script body executed
// inside a builder image with --privileged -v /dev:/dev. Used for pacstrap/debootstrap
// rootfs bootstrap and for VM disk-build chroots.
type PrivilegedRun struct {
	// Image is the builder image ref (e.g. arch-pacstrap-builder:CALVER).
	Image string
	// Script is the bash body executed inside the container. Run via
	// `bash -s <<'EOF' ... EOF` so quoting in the script body is preserved.
	Script string
	// Env lists KEY=VALUE pairs forwarded to the container.
	Env []string
	// Mounts lists "src:dst[:opts]" host-path bind mounts. /dev is always
	// added so loop devices created by losetup are visible.
	Mounts []string
	// OutputPath is an absolute path inside the container whose contents
	// must be copied out after the container exits successfully. May be
	// empty when the script writes directly to a host-bind-mounted path.
	OutputPath string
	// OutputDest is the absolute host path where OutputPath is written.
	// Required when OutputPath is set; ignored otherwise.
	OutputDest string
	// MinFreeBytes is the minimum free space required on BOTH the staging
	// directory (where the container writes the output) and the OutputDest
	// directory (where the output is copied). When > 0, RunPrivileged
	// statfs's both locations before the container runs and fails with a
	// clear error naming the directory, the free space, and the required
	// space — instead of the container dying cryptically mid-write with
	// "No space left on device". Callers that know their output size (a VM
	// disk, a rootfs tarball) MUST set it; the image-build bootstrap sets a
	// default floor.
	MinFreeBytes int64
}

// checkFreeSpace verifies that the filesystem holding path has at least
// minBytes of free space, returning an actionable error otherwise. The
// staging dir lands on the root fs (proc.MkdirTempHeld honors $TMPDIR), so a
// full disk used to surface only as a cryptic "gzip: stdout: No space left on
// device" from inside the container — this check turns that into a clear
// pre-flight failure naming the exact directory and the shortfall.
func checkFreeSpace(path string, minBytes int64) error {
	if minBytes <= 0 {
		return nil
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return fmt.Errorf("checking free space on %s: %w", path, err)
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	if free < minBytes {
		return fmt.Errorf("insufficient free space on %s: %.1f GiB free, %.1f GiB required (%.0f bytes free, %d bytes required); free disk space and retry",
			path, float64(free)/gib, float64(minBytes)/gib, float64(free), minBytes)
	}
	return nil
}

const gib = 1 << 30

// RootfsOutputFloor is the MinFreeBytes floor for a privileged rootfs build: the output is a
// gzipped rootfs tarball (typically 1-3 GiB for pacstrap/debootstrap), so require 4 GiB of free
// space on the staging + destination filesystems. A full disk used to fail cryptically inside
// the container ("gzip: stdout: No space left on device"). Shared by the OCI-image bootstrap
// (candy/plugin-build) and the VM-bootstrap engine (candy/plugin-vm) — one floor, R3.
const RootfsOutputFloor = 4 << 30

// RunPrivileged executes the container described by p. Returns an error when the container
// exits non-zero or when the output file capture fails. Stdout/stderr stream live to the
// caller. Always passes --privileged + --rm + -v /dev:/dev. Callers do NOT need to repeat
// those mounts in p.Mounts.
func RunPrivileged(p PrivilegedRun) error {
	if p.Image == "" {
		return fmt.Errorf("RunPrivileged: image is required")
	}
	if p.Script == "" {
		return fmt.Errorf("RunPrivileged: script is empty")
	}
	if p.OutputPath != "" && p.OutputDest == "" {
		return fmt.Errorf("RunPrivileged: OutputPath %q has no OutputDest", p.OutputPath)
	}

	stagingDir := ""
	hostStaging := ""
	// Always --user 0 because pacstrap / debootstrap / bootc install require root inside the
	// container (they call mount, mkfs, chroot, pacman-key, etc.). --privileged alone doesn't
	// override the image's USER directive.
	// -i required so podman attaches stdin to bash -s; without it the piped script is silently
	// dropped and the container exits immediately (no error, no stdout).
	// --net host because pacstrap / debootstrap / bootc install fetch packages over the
	// network from the host's mirrors. Rootful podman's default network mode (slirp/pasta)
	// doesn't always provide working outbound connectivity in privileged contexts.
	args := []string{"run", "--privileged", "--rm", "-i", "--user", "0", "--net", "host", "-v", "/dev:/dev"}
	for _, e := range p.Env {
		args = append(args, "-e", e)
	}
	for _, m := range p.Mounts {
		args = append(args, "-v", m)
	}
	if p.OutputPath != "" {
		// Bind-mount a host directory at the parent of OutputPath so the script can write
		// directly to OutputDest's location without a post-run copy step.
		var err error
		// Held for the privileged container's lifetime (pacstrap / bootc-install run for
		// minutes and write into the bind-mount, never touching this root's own mtime).
		var releaseStaging func()
		hostStaging, releaseStaging, err = proc.MkdirTempHeld("", "charly-priv-")
		if err != nil {
			return fmt.Errorf("creating staging dir: %w", err)
		}
		defer releaseStaging()
		defer proc.UnregisterTempCleanup(hostStaging)
		stagingDir = filepath.Dir(p.OutputPath)
		// Fail fast with a clear error before the container runs: the output is
		// written into this staging dir, and a full disk used to surface only as
		// a cryptic in-container "No space left on device".
		if err := checkFreeSpace(hostStaging, p.MinFreeBytes); err != nil {
			_ = os.RemoveAll(hostStaging)
			return err
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s", hostStaging, stagingDir))
	}
	args = append(args, p.Image, "bash", "-s")

	// Honor the runtime's engine.rootful setting. Rootless podman blocks pacstrap/bootc-install's
	// `mount /target/dev` even with --privileged because the user namespace has no CAP_SYS_ADMIN
	// equivalent for arbitrary bind mounts. `sudo podman` runs in the host namespace and bypasses
	// that constraint.
	bin := os.Getenv("CHARLY_PRIV_RUNNER")
	useSudo := false
	if bin == "" {
		bin = "podman"
		if rt, err := kit.ResolveRuntime(); err == nil && rt.Rootful == "sudo" {
			useSudo = true
		}
	}
	// When running via sudo, the rootful podman storage is independent of the user's rootless
	// storage. Locally-built images (the typical case for builder:pacstrap / builder:debootstrap)
	// won't be visible to `sudo podman run`, which would then fall back to a registry pull that
	// 403s for unpublished builder images. Stage the image into rootful storage first via
	// podman save | sudo podman load. Idempotent — TransferToRootful skips when the image already
	// exists in rootful storage. Covers BOTH image-build and VM-build, one shared runner (R3).
	if useSudo {
		if err := kit.TransferToRootful(p.Image); err != nil {
			if hostStaging != "" {
				_ = os.RemoveAll(hostStaging)
			}
			return fmt.Errorf("staging %s into rootful storage: %w", p.Image, err)
		}
	}
	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", append([]string{bin}, args...)...)
	} else {
		cmd = exec.Command(bin, args...)
	}
	cmd.Stdin = strings.NewReader(p.Script)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if os.Getenv("CHARLY_PRIV_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "+ %s %s\n", bin, strings.Join(args, " "))
		fmt.Fprintln(os.Stderr, "--- script begin ---")
		fmt.Fprintln(os.Stderr, p.Script)
		fmt.Fprintln(os.Stderr, "--- script end ---")
	}
	if err := cmd.Run(); err != nil {
		if hostStaging != "" {
			_ = os.RemoveAll(hostStaging)
		}
		return fmt.Errorf("privileged run %s failed: %w", p.Image, err)
	}

	if p.OutputPath != "" {
		// Copy the output from the staging dir to OutputDest.
		srcPath := filepath.Join(hostStaging, filepath.Base(p.OutputPath))
		if err := os.MkdirAll(filepath.Dir(p.OutputDest), 0o755); err != nil {
			_ = os.RemoveAll(hostStaging)
			return fmt.Errorf("creating output destination dir: %w", err)
		}
		// The copy materializes the same bytes on the OutputDest filesystem —
		// check it too, so a full OutputDest disk fails clearly instead of
		// mid-copy.
		if err := checkFreeSpace(filepath.Dir(p.OutputDest), p.MinFreeBytes); err != nil {
			_ = os.RemoveAll(hostStaging)
			return err
		}
		if err := CopyFileBytes(srcPath, p.OutputDest); err != nil {
			_ = os.RemoveAll(hostStaging)
			return fmt.Errorf("capturing privileged output %s -> %s: %w", srcPath, p.OutputDest, err)
		}
		_ = os.RemoveAll(hostStaging)
	}
	return nil
}

// CopyFileBytes copies src to dst, streaming (io.Copy) rather than reading the
// whole file into memory — the privileged outputs can be multi-GiB (a VM disk
// qcow2, a rootfs tarball), and the former os.ReadFile approach allocated the
// full file size in RAM, an OOM risk for a 20G+ disk.
func CopyFileBytes(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// RenderBootstrapScript renders the install template for a privileged bootstrap builder
// against a render context (the fields are documented at each call site: the OCI-image path
// in candy/plugin-build's runPrivilegedBootstrap, the VM-disk path in
// candy/plugin-vm/vm_bootstrap_engine.go's buildBootstrapRootfs).
func RenderBootstrapScript(builder *BuilderDef, ctx any) (string, error) {
	tmpl := BuilderPhaseTemplate(builder, spec.PhaseInstall, spec.VenueContainerBuilder)
	if tmpl == "" {
		return "", fmt.Errorf("builder has no phase.install.container template")
	}
	var buf bytes.Buffer
	t, err := template.New("bootstrap-script").Funcs(TemplateFuncs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing bootstrap script template: %w", err)
	}
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("rendering bootstrap script: %w", err)
	}
	return buf.String(), nil
}
