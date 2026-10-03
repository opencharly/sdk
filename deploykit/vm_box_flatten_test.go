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

	if v, err := isQcow2Overlay(base); err != nil || v {
		t.Errorf("isQcow2Overlay(base) = (%v,%v), want (false,nil) (a standalone image has no backing file)", v, err)
	}
	if v, err := isQcow2Overlay(overlay); err != nil || !v {
		t.Fatalf("isQcow2Overlay(overlay) = (%v,%v), want (true,nil) — the emitted disk IS a COW overlay", v, err)
	}

	flat := filepath.Join(dir, "flat.qcow2")
	if err := flattenQcow2(overlay, flat); err != nil {
		t.Fatalf("flattenQcow2: %v", err)
	}
	if v, err := isQcow2Overlay(flat); err != nil || v {
		t.Errorf("flattened image = (%v,%v), want (false,nil) — the box would remain host-coupled", v, err)
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

func TestIsQcow2Overlay_NonImageIsRawWholeDisk(t *testing.T) {
	requireQemuImg(t)
	// A NON-qcow2 file qemu-img can still read is reported `format: raw` with
	// exit 0 — a legitimate whole-disk image that needs no flattening, so the
	// caller correctly falls through to the plain COPY: (false, nil), no error.
	p := filepath.Join(t.TempDir(), "notaqcow2")
	if err := os.WriteFile(p, []byte("not a disk image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, err := isQcow2Overlay(p); err != nil || v {
		t.Errorf("isQcow2Overlay(raw non-image) = (%v,%v), want (false,nil) — a raw whole disk copies as-is", v, err)
	}
}

func TestIsQcow2Overlay_UnreadableErrorsClosed(t *testing.T) {
	requireQemuImg(t)
	// An image qemu-img CANNOT read (missing file, unreadable/corrupt) must NOT
	// be silently treated as "not an overlay": that is the fail-open which wraps
	// an unflattened COW disk — whose host-absolute backing file is absent on
	// every other host — straight into the box, the exact CrashLoop this change
	// fixes. It must return an error so EmitVmBox fails CLOSED.
	missing := filepath.Join(t.TempDir(), "does-not-exist.qcow2")
	v, err := isQcow2Overlay(missing)
	if err == nil {
		t.Fatalf("isQcow2Overlay(unreadable) = (%v,nil), want a non-nil error (fail CLOSED)", v)
	}
	if v {
		t.Errorf("isQcow2Overlay(unreadable) returned true with an error; want false")
	}
}
