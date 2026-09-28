package deploykit

// vm_box_flatten_test.go — R7 coverage for the self-contained-disk fix: a
// `charly vm build` disk is a copy-on-write qcow2 whose BACKING FILE is a
// host-absolute path under the build cache; a `FROM scratch` + COPY box carries
// only the overlay, so it is unusable on any other host (a KubeVirt node's
// containerd). isQcow2Overlay/flattenQcow2 make the emitted disk standalone.
//
// The test builds a REAL base + overlay with qemu-img and asserts the overlay is
// detected and that flattening removes the backing reference — it cannot pass
// without the fix.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireQemuImg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not installed — live tool required")
	}
}

func TestIsQcow2Overlay_DetectsBackingFile(t *testing.T) {
	requireQemuImg(t)
	dir := t.TempDir()

	base := filepath.Join(dir, "base.qcow2")
	overlay := filepath.Join(dir, "disk.qcow2")
	run := func(args ...string) {
		if out, err := exec.Command("qemu-img", args...).CombinedOutput(); err != nil {
			t.Fatalf("qemu-img %v: %v: %s", args, err, out)
		}
	}
	run("create", "-f", "qcow2", base, "64M")
	run("create", "-f", "qcow2", "-F", "qcow2", "-b", base, overlay, "64M")

	if isQcow2Overlay(base) {
		t.Errorf("isQcow2Overlay(base) = true, want false (a standalone image has no backing file)")
	}
	if !isQcow2Overlay(overlay) {
		t.Fatalf("isQcow2Overlay(overlay) = false, want true — the emitted disk IS a COW overlay")
	}

	flat := filepath.Join(dir, "flat.qcow2")
	if err := flattenQcow2(overlay, flat); err != nil {
		t.Fatalf("flattenQcow2: %v", err)
	}
	if isQcow2Overlay(flat) {
		t.Errorf("flattened image still reports a backing file — the box would remain host-coupled")
	}
	out, err := exec.Command("qemu-img", "info", "--output=json", flat).Output()
	if err != nil {
		t.Fatalf("qemu-img info flat: %v", err)
	}
	if strings.Contains(string(out), "backing-filename") {
		t.Errorf("flattened image JSON still carries backing-filename: %s", out)
	}
	if _, err := os.Stat(flat); err != nil {
		t.Fatalf("flattened image missing: %v", err)
	}
}

func TestIsQcow2Overlay_NonImageIsFalse(t *testing.T) {
	requireQemuImg(t)
	p := filepath.Join(t.TempDir(), "notaqcow2")
	if err := os.WriteFile(p, []byte("not a disk image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isQcow2Overlay(p) {
		t.Errorf("isQcow2Overlay(non-image) = true, want false (fail-open to the plain COPY)")
	}
}
