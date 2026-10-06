package deploykit

// vm_box_emit.go — the VM box image emitter + reader (cutover plan task 2, the VM
// analog of the pod-side WriteLabels / CapabilitiesFromLabels pair). A VM box is an
// OCI image whose labels carry the VM metadata contract and whose layer carries the
// disk artifact: the disk COPY is the layer, the labels are the contract.
//
// The metadata contract is spec.VmBoxMetadata riding ONE whole-struct JSON label —
// spec.LabelVmBox (ai.opencharly.vm.box), the VM analog of the per-field
// ai.opencharly.* family the pod side names via CapabilityLabelMap. The completeness
// gate lives spec-side (spec.VmBoxLabelMap + spec.CheckVmBoxLabelCompleteness, spec PR
// #90) and is exercised from TestVmBoxLabelCompleteness here so the sdk build breaks
// when a field is added without a label mapping.
//
// EmitVmBox (write side): scratch image, single COPY of the disk artifact to
// /disk.qcow2, plus ai.opencharly.version (the box CalVer) and ai.opencharly.description
// (JSON-encoded, like every JSON label the pod emitter writes) for human inspection.
// VmCapabilitiesFromLabels (read side): engine inspect → the ai.opencharly.vm.box
// label → unmarshal, the VM analog of CapabilitiesFromLabels' ExtractMetadata path.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/opencharly/spec/container"
	"github.com/opencharly/spec/shellquote"
	"github.com/opencharly/spec/spec"
)

// VM-box in-image disk paths. A VM box is an OCI image whose single layer holds the disk
// artifact; WHERE in that layer the disk sits is the CONSUMER's contract, so the two
// shipped callers name their path explicitly.
const (
	// VmBoxDiskPath is the path the charly VM-box reader (the from-box vm: deploy)
	// expects: /disk.qcow2. It is the DEFAULT EmitVmBox writes — unchanged behaviour.
	VmBoxDiskPath = "/disk.qcow2"

	// ContainerDiskPath is the KubeVirt containerDisk contract: the disk at
	// /disk/disk.img, the directory KubeVirt scans when no custom VMI `path:` is given.
	// EmitVmBoxAt writes here for a payload a KubeVirt cluster (or a Cua Fleet pool)
	// boots — the same path the published cua-omarchy-workspace artifact carries.
	ContainerDiskPath = "/disk/disk.img"
)

// EmitVmBox builds a VM box image in the local engine's storage from a materialized
// disk artifact + the VM metadata contract: a scratch image whose labels carry
// spec.VmBoxMetadata (whole-struct JSON on spec.LabelVmBox, plus version + description
// for inspect-ability) and whose single layer carries the disk at the DEFAULT VM-box
// path (/disk.qcow2).
//
// For a KubeVirt/containerDisk payload — the Cua Fleet shape, where the disk must live
// at /disk/disk.img (the directory KubeVirt scans) — use EmitVmBoxAt with
// ContainerDiskPath (or any explicit in-image path).
//
// The build context is the disk's own parent directory and the Containerfile is staged
// in a temp dir (passed via -f): the disk is never copied, buildah streams the file
// into the layer directly. The temp Containerfile is removed on return.
func EmitVmBox(engine, ref string, meta *spec.VmBoxMetadata, diskPath string) error {
	return EmitVmBoxAt(engine, ref, meta, diskPath, VmBoxDiskPath)
}

// validInImagePath reports whether p is a safe container-absolute path for the generated
// Containerfile's COPY destination. The destination is interpolated UNQUOTED into the
// build file (the Dockerfile parser strips matching quotes, and a container path with
// whitespace is pathological), so it must be validated rather than quoted: a whitespace
// or control character would either inject a second build instruction (`/disk/x\nRUN …`)
// or corrupt the COPY (a space reads as an extra source token). The rule is
// deliberately tight — a leading `/`, then only path-safe characters.
func validInImagePath(p string) bool {
	if !strings.HasPrefix(p, "/") || p == "/" {
		return false
	}
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/' || r == '.' || r == '-' || r == '_' || r == '+':
		default:
			return false
		}
	}
	return true
}

// EmitVmBoxAt is EmitVmBox with an explicit IN-IMAGE disk path. The VM-box path
// (/disk.qcow2) and the KubeVirt containerDisk path (/disk/disk.img) are the two
// shipped callers; any other layout is the author's choice. The path must be a safe
// container-absolute path (see validInImagePath) — it is written UNQUOTED into the
// Containerfile, so an unvalidated value is a build-instruction injection surface.
func EmitVmBoxAt(engine, ref string, meta *spec.VmBoxMetadata, diskPath, inImagePath string) error {
	if meta == nil {
		return fmt.Errorf("EmitVmBox: nil metadata")
	}
	if !validInImagePath(inImagePath) {
		return fmt.Errorf("EmitVmBox: in-image disk path %q must be a container-absolute path of [A-Za-z0-9._/+-] (no whitespace or control characters)", inImagePath)
	}
	absDisk, err := filepath.Abs(diskPath)
	if err != nil {
		return fmt.Errorf("EmitVmBox: resolving disk path %q: %w", diskPath, err)
	}
	if _, err := os.Stat(absDisk); err != nil {
		return fmt.Errorf("EmitVmBox: disk %q: %w", absDisk, err)
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("EmitVmBox: marshaling VmBoxMetadata: %w", err)
	}
	// The retention engine (candy/plugin-clean's charlyImageTags) GROUPS local images by
	// the ai.opencharly.box label; the pod-side WriteLabels stamps it, but this VM emitter
	// did not — so every `charly vm build` box tag was INVISIBLE to retention and grew
	// unbounded (measured: 24 tags of one bed box; opencharly/charly#808). Stamp it here
	// with the same value convention (spec.LeafName of the ref) so a VM box tag is
	// reclaimable by `charly clean` / the post-build prune, exactly like a pod box.
	boxLabel := spec.LeafName(ref)

	dir, err := os.MkdirTemp("", "vm-box-emit-*")
	if err != nil {
		return fmt.Errorf("EmitVmBox: staging Containerfile: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// The disk the box carries MUST be SELF-CONTAINED. `charly vm build` produces a
	// copy-on-write overlay whose BACKING FILE is a host-absolute path under the
	// build cache; a `FROM scratch` + COPY box carries only the overlay, so on any
	// other host (a KubeVirt node's containerd) the backing file is absent and the
	// guest cannot open the disk. Flatten the overlay into a standalone qcow2 first
	// (qemu-img convert), so the COPY is the whole artifact.
	diskBase := filepath.Base(absDisk)
	overlay, err := isQcow2Overlay(absDisk)
	if err != nil {
		return fmt.Errorf("EmitVmBox: inspecting disk %q: %w", absDisk, err)
	}
	if overlay {
		flat := filepath.Join(dir, diskBase)
		if err := flattenQcow2(absDisk, flat); err != nil {
			return fmt.Errorf("EmitVmBox: flattening disk %q: %w", absDisk, err)
		}
		absDisk = flat
	}

	cf := renderVmBoxContainerfile(diskBase, inImagePath, metaJSON, meta, boxLabel)

	cfPath := filepath.Join(dir, "Containerfile")
	if err := os.WriteFile(cfPath, []byte(cf), 0o644); err != nil {
		return fmt.Errorf("EmitVmBox: writing Containerfile: %w", err)
	}

	binary := container.EngineBinary(engine)
	build := exec.Command(binary, "build", "-t", ref, "-f", cfPath, filepath.Dir(absDisk))
	out, err := build.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("EmitVmBox: %s build -t %s: %w: %s", binary, ref, err, msg)
	}
	return nil
}

// isQcow2Overlay reports whether path is a qcow2 image that carries a BACKING
// file (a copy-on-write overlay). Such an image is NOT self-contained — its
// backing path is host-absolute — so it must be flattened before being wrapped
// into a box/containerDisk. A non-qcow2 path (a raw/whole-disk image) is
// reported (false, nil): the caller falls through to the plain COPY. An
// UNREADABLE image is reported as an error, never a silent false — a failed
// `qemu-img info` must fail the emit CLOSED rather than wrap an unflattened
// overlay whose backing file is missing on every other host (the exact
// CrashLoop this PR exists to prevent).
func isQcow2Overlay(path string) (bool, error) {
	// Only a QCow2 image can be a COW overlay. Read the magic first: a NON-qcow2
	// artifact (a raw/whole-disk image, or a fixture) is not an overlay and needs
	// no `qemu-img` — so `EmitVmBox` never shells out for it, and a host/CI without
	// `qemu-img` still emits a raw disk (it just cannot flatten a real overlay).
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("inspecting disk %s: %w", path, err)
	}
	magic := make([]byte, 4)
	_, rerr := io.ReadFull(f, magic)
	if cerr := f.Close(); cerr != nil {
		return false, fmt.Errorf("inspecting disk %s: %w", path, cerr)
	}
	if rerr != nil {
		// A genuine short read (EOF) is a file too small to hold an image header —
		// not a qcow2 overlay, so the emit COPYs it as-is. Any OTHER read error
		// (EISDIR from an os.Open'd directory, EIO, …) is a genuinely UNREADABLE
		// disk and must fail CLOSED, never a silent false.
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, fmt.Errorf("inspecting disk %s: %w", path, rerr)
	}
	if string(magic) != "QFI\xfb" {
		return false, nil // not qcow2 (raw image / fixture): no overlay, no flatten
	}
	// A qcow2 image: ask qemu-img whether it carries a backing file. An unreadable
	// qcow2 fails CLOSED — never a silent false, which would wrap an unflattened
	// COW disk (host-absolute backing, absent elsewhere) straight into the box.
	out, err := exec.Command("qemu-img", "info", "--output=json", path).Output()
	if err != nil {
		return false, fmt.Errorf("qemu-img info %s: %w", path, err)
	}
	return strings.Contains(string(out), `"backing-filename"`), nil
}

// flattenQcow2 writes a SELF-CONTAINED qcow2 copy of src to dst (qemu-img
// convert), dropping any backing-file reference so the result stands alone.
func flattenQcow2(src, dst string) error {
	out, err := exec.Command("qemu-img", "convert", "-O", "qcow2", src, dst).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("qemu-img convert %s -> %s: %w: %s", src, dst, err, msg)
	}
	return nil
}

// renderVmBoxContainerfile is the PURE half of the emitter: the scratch image, the disk
// COPY at the requested in-image path, and the metadata LABELs. Split out so the
// in-image path contract (the VM-box /disk.qcow2 vs the KubeVirt containerDisk
// /disk/disk.img) is unit-testable with no container engine.
func renderVmBoxContainerfile(diskBase, inImagePath string, metaJSON []byte, meta *spec.VmBoxMetadata, boxLabel string) string {
	var cf strings.Builder
	cf.WriteString("# VM box image - generated by deploykit.EmitVmBox (do not edit)\n")
	cf.WriteString("FROM scratch\n")
	// The disk COPY is the artifact layer. Double-quote the source only when the
	// basename needs it (Dockerfile COPY strips matching double quotes).
	fmt.Fprintf(&cf, "COPY %s %s\n", copyQuote(diskBase), inImagePath)
	// The labels are the contract. ai.opencharly.box is the RETENTION GROUP KEY (the value
	// the pod-side WriteLabels stamps, so local-image retention can find and reclaim the
	// tag); ai.opencharly.vm.box always; description is conditional (omitted when empty),
	// mirroring WriteLabels.
	if boxLabel != "" {
		fmt.Fprintf(&cf, "LABEL %s=%q\n", spec.LabelBox, boxLabel)
	}
	fmt.Fprintf(&cf, "LABEL %s=%s\n", spec.LabelVmBox, shellquote.ShellQuote(string(metaJSON)))
	if meta.Description != "" {
		descJSON, _ := json.Marshal(meta.Description)
		fmt.Fprintf(&cf, "LABEL %s=%s\n", spec.LabelDescription, shellquote.ShellQuote(string(descJSON)))
	}
	return cf.String()
}

// VmCapabilitiesFromLabels reads the VM metadata contract back from a VM box image in
// local engine storage: engine inspect → the ai.opencharly.vm.box label → unmarshal
// into *spec.VmBoxMetadata. The VM analog of CapabilitiesFromLabels (deploykit); the
// source-less VM deploy (`charly deploy from-box vm:<ref>`) reconstructs every field from
// the pushed box image via this function.
func VmCapabilitiesFromLabels(engine, imageRef string) (*spec.VmBoxMetadata, error) {
	labels, err := container.InspectLabels(engine, imageRef)
	if err != nil {
		if !container.LocalImageExists(engine, imageRef) {
			return nil, fmt.Errorf("%w: %s", spec.ErrImageNotLocal, imageRef)
		}
		return nil, err
	}
	raw := labels[spec.LabelVmBox]
	if raw == "" {
		return nil, fmt.Errorf("image %q has no %s label (not a VM box image?)", imageRef, spec.LabelVmBox)
	}
	var meta spec.VmBoxMetadata
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("parsing %s from %s: %w", spec.LabelVmBox, imageRef, err)
	}
	return &meta, nil
}

// copyQuote wraps a COPY source in double quotes when its basename contains whitespace
// (the only case the Dockerfile parser requires quoting); plain names stay bare.
func copyQuote(name string) string {
	if strings.ContainsAny(name, " \t") {
		return "\"" + strings.ReplaceAll(name, "\"", "\\\"") + "\""
	}
	return name
}
