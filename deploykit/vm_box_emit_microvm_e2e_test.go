package deploykit

// vm_box_emit_microvm_e2e_test.go — LIVE proof of the MICROVM shape: it builds a REAL box with
// the REAL container engine from a root drive AND a kernel that live in DIFFERENT directories
// (the case that needs a staged build context), then extracts BOTH artefacts from the image and
// reads the metadata label back. Skips cleanly when the engine is absent — live or skip, never
// mocked (R8).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestEmitVmBoxMicroVM_Live(t *testing.T) {
	requireUsableEngine(t)
	requireBuildkitDrive(t)

	dir := t.TempDir()
	rootfs := filepath.Join(dir, "rootfs.img")
	kernelDir := filepath.Join(dir, "kernel") // a DIFFERENT directory: needs a staged context
	if err := os.MkdirAll(kernelDir, 0o755); err != nil {
		t.Fatal(err)
	}

	kernel := filepath.Join(kernelDir, "vmlinux")

	const rootfsBody, kernelBody = "ROOTFS-ARTEFACT-BODY", "KERNEL-ARTEFACT-BODY"
	if err := os.WriteFile(rootfs, []byte(rootfsBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kernel, []byte(kernelBody), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := &spec.VmBoxMetadata{
		Distro: "ubuntu",
		Arch:   "x86_64",
		Init:   "systemd",
		Source: spec.VmBoxSource{
			Kind:    "rootfs",
			Image:   "ghcr.io/opencharly/fireactions-runner:v0.1",
			Format:  "ext4",
			DiskMiB: 6144,
		},
		Description: "microVM e2e",
	}

	ref := "localhost/oc-vmbox-microvm-e2e:test"
	defer exec.Command("podman", "rmi", "-f", ref).Run() //nolint:errcheck

	if err := EmitVmBoxMicroVM(context.Background(), "podman", ref, meta, rootfs, kernel); err != nil {
		t.Fatalf("EmitVmBoxMicroVM: %v", err)
	}

	if meta.Artifacts.RootfsPath != MicroVMRootfsPath || meta.Artifacts.KernelPath != MicroVMKernelPath {
		t.Fatalf("recorded layout = %+v, want the shipped microVM paths %s / %s",
			meta.Artifacts, MicroVMRootfsPath, MicroVMKernelPath)
	}

	cid := createContainer(t, ref)

	defer exec.Command("podman", "rm", "-f", cid).Run() //nolint:errcheck

	// (a) THE LAYER: both artefacts are in the image, at the recorded paths, byte for byte.
	for _, c := range []struct{ name, inImage, want string }{
		{"rootfs", meta.Artifacts.RootfsPath, rootfsBody},
		{"kernel", meta.Artifacts.KernelPath, kernelBody},
	} {
		dest := filepath.Join(dir, "extracted-"+c.name)
		if out, err := exec.Command("podman", "cp", cid+":"+c.inImage, dest).CombinedOutput(); err != nil {
			t.Fatalf("podman cp %s: %v: %s", c.inImage, err, out)
		}

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != c.want {
			t.Errorf("%s at %s = %q, want %q", c.name, c.inImage, got, c.want)
		}
	}

	// (b) THE LABEL: a source-less reader gets the same layout AND the provenance back — what a
	// `charly deploy from-box` has to reconstruct the boot from, with no source repo in reach.
	read, err := VmCapabilitiesFromLabels("podman", ref)
	if err != nil {
		t.Fatalf("VmCapabilitiesFromLabels: %v", err)
	}

	if read.Artifacts != meta.Artifacts {
		t.Errorf("label layout = %+v, want %+v", read.Artifacts, meta.Artifacts)
	}

	if read.Source.Kind != "rootfs" || read.Source.Format != "ext4" || read.Source.DiskMiB != 6144 {
		t.Errorf("label provenance = %+v, want the rootfs arm carrying its format and built size", read.Source)
	}

	// (c) THE STAGING RULE: a same-filesystem source is LINKED into the context, never moved or
	// destroyed, so the files the caller handed in are still there afterwards.
	for name, path := range map[string]string{"rootfs": rootfs, "kernel": kernel} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the emitted %s was removed from its source path: %v", name, err)
		}
	}
}
