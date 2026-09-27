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
	"fmt"
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

	dir, err := os.MkdirTemp("", "vm-box-emit-*")
	if err != nil {
		return fmt.Errorf("EmitVmBox: staging Containerfile: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	cf := renderVmBoxContainerfile(filepath.Base(absDisk), inImagePath, metaJSON, meta)

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

// renderVmBoxContainerfile is the PURE half of the emitter: the scratch image, the disk
// COPY at the requested in-image path, and the metadata LABELs. Split out so the
// in-image path contract (the VM-box /disk.qcow2 vs the KubeVirt containerDisk
// /disk/disk.img) is unit-testable with no container engine.
func renderVmBoxContainerfile(diskBase, inImagePath string, metaJSON []byte, meta *spec.VmBoxMetadata) string {
	var cf strings.Builder
	cf.WriteString("# VM box image - generated by deploykit.EmitVmBox (do not edit)\n")
	cf.WriteString("FROM scratch\n")
	// The disk COPY is the artifact layer. Double-quote the source only when the
	// basename needs it (Dockerfile COPY strips matching double quotes).
	fmt.Fprintf(&cf, "COPY %s %s\n", copyQuote(diskBase), inImagePath)
	// The labels are the contract. ai.opencharly.vm.box always; version and
	// description are conditional (omitted when empty), mirroring WriteLabels.
	fmt.Fprintf(&cf, "LABEL %s=%s\n", spec.LabelVmBox, shellquote.ShellQuote(string(metaJSON)))
	if meta.Version != "" {
		fmt.Fprintf(&cf, "LABEL %s=%s\n", spec.LabelVersion, shellquote.ShellQuote(meta.Version))
	}
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
