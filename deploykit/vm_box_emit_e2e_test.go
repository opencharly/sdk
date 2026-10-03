package deploykit

// vm_box_emit_e2e_test.go — LIVE end-to-end proof of the flatten path: it calls
// EmitVmBoxAt on a REAL COW overlay with the REAL container engine, then extracts
// the disk the emitted box carries and asserts (a) it is NOT zero-length (i.e. the
// flatten target did NOT collide with / truncate the source) and (b) it carries no
// backing file (self-contained). Skips cleanly when podman/qemu-img are absent.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestEmitVmBoxAt_OverlayIsFlattenedIntoSelfContainedBox(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not installed — live tool required")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed — live engine required")
	}
	dir := t.TempDir()

	base := filepath.Join(dir, "base.qcow2")
	overlay := filepath.Join(dir, "disk.qcow2")
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", base, "32M").CombinedOutput(); err != nil {
		t.Fatalf("create base: %v: %s", err, out)
	}
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", base, overlay, "32M").CombinedOutput(); err != nil {
		t.Fatalf("create overlay: %v: %s", err, out)
	}
	// The overlay MUST carry a backing file — otherwise the test proves nothing.
	if v, err := isQcow2Overlay(overlay); err != nil || !v {
		t.Fatalf("precondition: isQcow2Overlay(overlay)=(%v,%v), want (true,nil)", v, err)
	}

	ref := "localhost/oc-vmboxe2e:test"
	defer exec.Command("podman", "rmi", "-f", ref).Run() //nolint:errcheck

	if err := EmitVmBoxAt("podman", ref, &spec.VmBoxMetadata{Description: "e2e"}, overlay, VmBoxDiskPath); err != nil {
		t.Fatalf("EmitVmBoxAt: %v", err)
	}

	cidOut, err := exec.Command("podman", "create", ref).Output()
	if err != nil {
		t.Fatalf("podman create: %v", err)
	}
	cid := strings.TrimSpace(string(cidOut))
	defer exec.Command("podman", "rm", "-f", cid).Run() //nolint:errcheck

	extracted := filepath.Join(dir, "extracted.qcow2")
	if out, err := exec.Command("podman", "cp", cid+":"+VmBoxDiskPath, extracted).CombinedOutput(); err != nil {
		t.Fatalf("podman cp: %v: %s", err, out)
	}
	fi, err := os.Stat(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() == 0 {
		t.Fatalf("emitted disk is ZERO-LENGTH — the flatten target collided with/truncated the source")
	}
	info, err := exec.Command("qemu-img", "info", extracted).CombinedOutput()
	if err != nil {
		t.Fatalf("qemu-img info on emitted disk: %v: %s", err, info)
	}
	if strings.Contains(string(info), "backing file:") {
		t.Fatalf("emitted disk still has a backing file — not self-contained:\n%s", info)
	}
	t.Logf("emitted disk: %d bytes, self-contained\n%s", fi.Size(), info)
}
