package deploykit

// vm_box_emit_test.go — the VM-box metadata contract tests for the sdk half of the
// VM-box cutover (plan task 2). Two pure contract tests (the label wire round-trip +
// the struct ↔ label completeness gate — the sdk build breaks when a VmBoxMetadata
// field is added without a VmBoxLabelMap entry) and one live integration test that
// EMITS a VM box image (EmitVmBox: scratch image + disk layer + metadata labels),
// reads it back (VmCapabilitiesFromLabels), and asserts equality — proving the disk
// COPY is the layer and the labels are the contract end to end on local podman
// storage. The integration test skips when podman is unavailable (t.Skip).

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/spec/spec"
)

// TestVmBoxMetadataLabelRoundTrip proves the whole VmBoxMetadata struct round-trips
// through its single JSON OCI label (ai.opencharly.vm.box): marshal → unmarshal is an
// identity. EmitVmBox writes exactly this JSON as the label value and
// VmCapabilitiesFromLabels reads the struct back from it — this test pins that the Go
// wire form survives the trip (sdk-side mirror of spec's TestVmBoxMetadataLabelRoundTrip,
// pinned through the sdk's go.mod spec require).
func TestVmBoxMetadataLabelRoundTrip(t *testing.T) {
	in := spec.VmBoxMetadata{
		Distro:        "fedora",
		Arch:          "x86_64",
		BaseUser:      "fedora",
		SSHUser:       "charly",
		Firmware:      "uefi-secure",
		Init:          "systemd",
		CharlyInstall: "scp",
		Version:       "0.2026245.0",
		Source: spec.VmBoxSource{
			Kind:         "clone",
			FromVm:       "base-vm",
			FromSnapshot: "snap-1",
		},
		Description: "fedora 43 layered base VM",
		Plan: []spec.Step{
			{Run: "true"},
		},
	}

	wire, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal VmBoxMetadata: %v", err)
	}

	var out spec.VmBoxMetadata
	if err := json.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal VmBoxMetadata: %v\nwire: %s", err, wire)
	}

	if !reflect.DeepEqual(in, out) {
		t.Errorf("VmBoxMetadata did not round-trip through JSON:\n in: %+v\nout: %+v", in, out)
	}
}

// TestVmBoxLabelCompleteness — every exported field on spec.VmBoxMetadata must have a
// VmBoxLabelMap entry (the struct ↔ label sync table in spec). Adding a field without
// a mapping breaks the build here, enforcing the invariant "every VM-box metadata field
// rides the ai.opencharly.vm.box label" so VmCapabilitiesFromLabels can reconstruct the
// full contract from a pushed box image.
func TestVmBoxLabelCompleteness(t *testing.T) {
	if err := spec.CheckVmBoxLabelCompleteness(); err != nil {
		t.Fatal(err)
	}
}

// TestVmBoxEmitReadBackRoundTrip is the live integration test: EmitVmBox a tiny
// fixture "disk" (a 1-byte file) with a populated metadata contract into local podman
// storage, read the contract back with VmCapabilitiesFromLabels, and assert equality.
// Skips when podman is not available on the host.
func TestVmBoxEmitReadBackRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skipf("podman not available on this host — skipping VM box emit/read-back integration test: %v", err)
	}

	diskPath := filepath.Join(t.TempDir(), "disk.qcow2")
	if err := os.WriteFile(diskPath, []byte{0x01}, 0o644); err != nil {
		t.Fatalf("writing fixture disk: %v", err)
	}

	meta := &spec.VmBoxMetadata{
		Distro:        "fedora",
		Arch:          "x86_64",
		BaseUser:      "fedora",
		SSHUser:       "charly",
		Firmware:      "uefi-secure",
		Init:          "systemd",
		CharlyInstall: "scp",
		Version:       "0.2026246.0",
		Source: spec.VmBoxSource{
			Kind: "bootc",
			Box:  "ghcr.io/opencharly/fedora:43",
		},
		Description: "fedora 43 VM box emitted by the sdk",
		Plan: []spec.Step{
			{Run: "true"},
		},
	}

	// Unique ref per run: the label inspect is cache-backed (TTL), so a fresh name
	// guarantees the read hits the image this test just built.
	ref := "localhost/vm-box-emit-test:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	t.Cleanup(func() {
		_ = exec.Command("podman", "rmi", "-f", ref).Run()
	})

	if err := EmitVmBox("podman", ref, meta, diskPath); err != nil {
		t.Fatalf("EmitVmBox: %v", err)
	}

	got, err := VmCapabilitiesFromLabels("podman", ref)
	if err != nil {
		t.Fatalf("VmCapabilitiesFromLabels: %v", err)
	}
	if got == nil {
		t.Fatal("VmCapabilitiesFromLabels returned nil metadata")
	}
	if !reflect.DeepEqual(got, meta) {
		t.Errorf("VM box metadata did not round-trip through the emitted image:\n in: %+v\nout: %+v", meta, got)
	}

	// The disk COPY must be the image's single layer — the layer is the artifact.
	out, err := exec.Command("podman", "image", "inspect", "--format", "{{len .RootFS.Layers}}", ref).Output()
	if err != nil {
		t.Fatalf("inspecting layer count of %s: %v", ref, err)
	}
	if got := string(out); got != "1\n" {
		t.Errorf("emitted VM box image has %q layers, want 1 (the disk COPY)", got)
	}
}

// TestEmitVmBoxAtContainerDiskPathLive is the LIVE proof of the NEW path: EmitVmBoxAt
// with ContainerDiskPath emits a real image whose single layer holds the disk at
// /disk/disk.img — the KubeVirt containerDisk contract. The emitted image is saved to an
// OCI layout and the layer tar inspected directly (the scratch image has no shell to run
// `find` in). Skips when podman is unavailable.
func TestEmitVmBoxAtContainerDiskPathLive(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skipf("podman not available on this host — skipping the live ContainerDiskPath emit: %v", err)
	}

	diskPath := filepath.Join(t.TempDir(), "disk.qcow2")
	if err := os.WriteFile(diskPath, []byte("qcow2-fixture-payload"), 0o644); err != nil {
		t.Fatalf("writing fixture disk: %v", err)
	}
	meta := &spec.VmBoxMetadata{Version: "0.2026269.0"}
	ref := "localhost/vm-box-containerdisk-live:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	t.Cleanup(func() { _ = exec.Command("podman", "rmi", "-f", ref).Run() })

	if err := EmitVmBoxAt("podman", ref, meta, diskPath, ContainerDiskPath); err != nil {
		t.Fatalf("EmitVmBoxAt(ContainerDiskPath): %v", err)
	}

	// Save the emitted image as an OCI layout and read the layer tar's entry names.
	layout := t.TempDir()
	if out, err := exec.Command("podman", "save", "--format", "oci-dir", "-o", layout, ref).CombinedOutput(); err != nil {
		t.Fatalf("podman save: %v\n%s", err, out)
	}
	names := layerEntryNames(t, layout)
	if !contains(names, "disk/disk.img") {
		t.Errorf("the emitted containerDisk layer does not hold disk/disk.img; entries: %v", names)
	}
	if contains(names, "disk.qcow2") {
		t.Errorf("the containerDisk payload must NOT carry the VM-box default disk.qcow2; entries: %v", names)
	}
}

// layerEntryNames returns the tar entry names of the single layer blob in an OCI image
// layout directory (podman save --format oci-dir lays blobs under blobs/<algorithm>/).
func layerEntryNames(t *testing.T, dir string) []string {
	t.Helper()
	for _, blobDir := range []string{filepath.Join(dir, "blobs", "sha256"), filepath.Join(dir, "blobs"), dir} {
		entries, err := os.ReadDir(blobDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			f, err := os.Open(filepath.Join(blobDir, e.Name()))
			if err != nil {
				continue
			}
			names, ok := tarNames(f)
			_ = f.Close()
			if ok && len(names) > 0 {
				return names
			}
		}
	}
	t.Fatalf("no tar layer found in OCI layout %s", dir)
	return nil
}

// tarNames reads a (possibly gzip-compressed) tar stream's entry names; ok=false when the
// stream is not a tar.
func tarNames(r io.Reader) ([]string, bool) {
	br := bufio.NewReader(r)
	head, _ := br.Peek(2)
	var src io.Reader = br
	if len(head) == 2 && head[0] == 0x1f && head[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, false
		}
		defer func() { _ = gz.Close() }()
		src = gz
	}
	tr := tar.NewReader(src)
	var names []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, hdr.Name)
	}
	return names, len(names) > 0
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestRenderVmBoxContainerfilePaths pins the in-image path contract both callers depend
// on: EmitVmBox writes /disk.qcow2 (the charly VM-box reader's path) and EmitVmBoxAt
// writes whatever the caller names — for a KubeVirt containerDisk / Cua Fleet payload,
// /disk/disk.img (the directory KubeVirt scans). Pure: no container engine.
func TestRenderVmBoxContainerfilePaths(t *testing.T) {
	meta := &spec.VmBoxMetadata{Version: "0.2026269.0"}
	j, _ := json.Marshal(meta)

	vmBox := renderVmBoxContainerfile("disk.qcow2", VmBoxDiskPath, j, meta)
	if !strings.Contains(vmBox, "COPY disk.qcow2 /disk.qcow2\n") {
		t.Errorf("the default VM-box render must COPY to %s; got:\n%s", VmBoxDiskPath, vmBox)
	}
	if strings.Contains(vmBox, ContainerDiskPath) {
		t.Errorf("the default VM-box render must NOT carry the containerDisk path:\n%s", vmBox)
	}

	cd := renderVmBoxContainerfile("disk.img", ContainerDiskPath, j, meta)
	if !strings.Contains(cd, "COPY disk.img /disk/disk.img\n") {
		t.Errorf("the containerDisk render must COPY to %s; got:\n%s", ContainerDiskPath, cd)
	}
	if strings.Contains(cd, VmBoxDiskPath) {
		t.Errorf("the containerDisk render must NOT carry the VM-box path:\n%s", cd)
	}
}

// TestEmitVmBoxAtRejectsRelativePath — the in-image path is a container-absolute path; a
// relative one would land relative to the container root only by buildah accident.
func TestEmitVmBoxAtRejectsRelativePath(t *testing.T) {
	meta := &spec.VmBoxMetadata{Version: "0.2026269.0"}
	disk := filepath.Join(t.TempDir(), "d.qcow2")
	if err := os.WriteFile(disk, []byte{1}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EmitVmBoxAt("podman", "localhost/x:t", meta, disk, "disk/disk.img"); err == nil {
		t.Error("EmitVmBoxAt accepted a relative in-image path; it must be absolute")
	}
}

// TestEmitVmBoxAtRejectsUnsafePath — the destination is written UNQUOTED into the
// generated Containerfile, so a whitespace/control character is a build-instruction
// injection surface (`/disk/x\nRUN <cmd>`). Every such value must be REJECTED, not emitted.
func TestEmitVmBoxAtRejectsUnsafePath(t *testing.T) {
	meta := &spec.VmBoxMetadata{Version: "0.2026269.0"}
	disk := filepath.Join(t.TempDir(), "d.qcow2")
	if err := os.WriteFile(disk, []byte{1}, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"/disk/x\nRUN echo pwned", // newline → extra instruction
		"/disk/disk.img extra",    // space → extra COPY source token
		"/disk/tab\timg",          // tab
		"/disk/$(whoami).img",     // shell-ish (rejected by the charset)
		"/",                       // root alone is not a file path
		"",                        // empty
		"/disk/disk.img\"",        // quote
		"/disk/disk.img;rm -rf /", // semicolon
	} {
		if err := EmitVmBoxAt("podman", "localhost/x:t", meta, disk, bad); err == nil {
			t.Errorf("EmitVmBoxAt accepted unsafe in-image path %q", bad)
		}
	}
	// And the valid chars are accepted (no false rejection of the shipped paths).
	for _, ok := range []string{"/disk/disk.img", "/custom-disk-path/fedora25.qcow2", "/a_b/c-d+e.f"} {
		if !validInImagePath(ok) {
			t.Errorf("validInImagePath rejected the valid path %q", ok)
		}
	}
}
