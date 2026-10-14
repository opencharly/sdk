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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/opencharly/sdk/buildkitdrive"
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

	// MicroVMRootfsPath / MicroVMKernelPath are the MICROVM contract: the root drive the
	// VMM attaches and the kernel it boots with, delivered TOGETHER in one box (the
	// `rootfs`/`kernel` provenance arms and the #VmBoxArtifacts layout record, opencharly/spec#229).
	// The emitter records exactly these paths in spec.VmBoxMetadata.Artifacts, so a reader
	// is TOLD where the layer carries each file instead of inferring it from one constant.
	MicroVMRootfsPath = "/rootfs.img"
	MicroVMKernelPath = "/vmlinux"
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
func EmitVmBox(ctx context.Context, engine, ref string, meta *spec.VmBoxMetadata, diskPath string) error {
	return EmitVmBoxAt(ctx, engine, ref, meta, diskPath, VmBoxDiskPath)
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
func EmitVmBoxAt(ctx context.Context, engine, ref string, meta *spec.VmBoxMetadata, diskPath, inImagePath string) error {
	return emitVmBoxArtifacts(ctx, engine, ref, meta, []vmBoxArtifact{{src: diskPath, inImage: inImagePath}})
}

// EmitVmBoxMicroVM emits the MICROVM shape: the root drive the VMM attaches AND the
// kernel it boots with, delivered together in one box at the shipped microVM paths. The
// layout it WROTE is recorded in spec.VmBoxMetadata.Artifacts only AFTER the build
// succeeds — the label must never claim a path the layer does not carry — and the
// provenance (#VmBoxSource: the `rootfs` arm with its origin ref and built size, the
// `kernel` arm with its digest and command line) is the CALLER's to fill, because only
// the caller knows where the artefacts came from.
func EmitVmBoxMicroVM(ctx context.Context, engine, ref string, meta *spec.VmBoxMetadata, rootfsPath, kernelPath string) error {
	if meta == nil {
		return fmt.Errorf("EmitVmBoxMicroVM: nil metadata")
	}

	emit := *meta
	emit.Artifacts = spec.VmBoxArtifacts{RootfsPath: MicroVMRootfsPath, KernelPath: MicroVMKernelPath}

	if err := emitVmBoxArtifacts(ctx, engine, ref, &emit, []vmBoxArtifact{
		{src: rootfsPath, inImage: MicroVMRootfsPath},
		{src: kernelPath, inImage: MicroVMKernelPath},
	}); err != nil {
		return err
	}

	meta.Artifacts = emit.Artifacts

	return nil
}

// vmBoxArtifact is one file a box delivers: its host path and its destination in the image.
type vmBoxArtifact struct {
	src     string
	inImage string
}

// vmBoxCopy is one COPY instruction: the artefact's name IN THE BUILD CONTEXT and its
// destination in the image.
type vmBoxCopy struct {
	contextName string
	inImage     string
}

// emitVmBoxArtifacts is the ONE emitter both shapes share (R2) — one validation, one
// staging rule, one render, one build. It validates every in-image path BEFORE touching
// the filesystem (each is written UNQUOTED into the Containerfile, so an unsafe value is
// a build-instruction injection surface), resolves and stats every source, and stages a
// build context that carries all of them.
//
// The context is the artefacts' OWN directory whenever they share one and nothing had to
// be rewritten — exactly the behaviour the single-disk callers have always had, where the
// file is never copied because buildah streams it into the layer from its own directory.
// Otherwise the context is a staging directory in which any qcow2 overlay has been
// flattened and every other artefact LINKED into it, so a multi-gigabyte rootfs is still
// copied; only a link the filesystem refuses falls back to a byte copy.
func emitVmBoxArtifacts(ctx context.Context, engine, ref string, meta *spec.VmBoxMetadata, arts []vmBoxArtifact) error {
	if meta == nil {
		return fmt.Errorf("EmitVmBox: nil metadata")
	}
	if len(arts) == 0 {
		return fmt.Errorf("EmitVmBox: no artefacts to deliver")
	}

	for _, a := range arts {
		if !validInImagePath(a.inImage) {
			return fmt.Errorf("EmitVmBox: in-image path %q must be a container-absolute path of [A-Za-z0-9._/+-] (no whitespace or control characters)", a.inImage)
		}
	}

	resolved, err := resolveVmBoxArtifacts(arts)
	if err != nil {
		return err
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("EmitVmBox: marshaling VmBoxMetadata: %w", err)
	}
	// The retention engine (candy/plugin-clean's charlyImageTags) GROUPS local images by
	// the ai.opencharly.box label; the pod-side WriteLabels stamps it, but this VM emitter
	// did not — so every `charly vm build` box tag was INVISIBLE to retention and grew
	// unbounded (measured: 24 tags of one bed box; opencharly/charly#808). Stamp it here
	// with the SAME value convention (the repository's leaf name) so a VM box tag is
	// reclaimable by `charly clean` / the post-build prune, exactly like a pod box.
	boxLabel := repoLeaf(ref)

	dir, err := os.MkdirTemp("", "vm-box-emit-*")
	if err != nil {
		return fmt.Errorf("EmitVmBox: staging Containerfile: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	ctxDir, copies, err := stageVmBoxArtifacts(dir, resolved)
	if err != nil {
		return err
	}

	cf := renderVmBoxContainerfile(copies, metaJSON, meta, boxLabel)

	cfPath := filepath.Join(dir, "Containerfile")
	if err := os.WriteFile(cfPath, []byte(cf), 0o644); err != nil {
		return fmt.Errorf("EmitVmBox: writing Containerfile: %w", err)
	}

	// ⚑ BuildKit is the builder (the cutover's ONE builder), and the image has to be
	// BRIDGED into the run engine's store: the daemon writes into its OWN content store,
	// and every later read of this image — its capability labels, its deploy, its status,
	// `charly clean` — resolves it through the RUN engine. The two stores are not shared.
	stageDir, err := buildkitdrive.StageDir()
	if err != nil {
		return err
	}
	dest := buildkitdrive.ExportFile(stageDir, ref)
	// DaemonOptions{} is deliberate here: this path has no parallelism input of its own, so
	// it expresses NO OPINION and never restarts a daemon the build drive configured.
	daemon, err := buildkitdrive.Ensure(ctx, buildkitdrive.ExecRunner{}, buildkitdrive.DaemonOptions{})
	if err != nil {
		return fmt.Errorf("EmitVmBox: buildkit daemon: %w", err)
	}
	args, err := buildkitdrive.Build{
		Containerfile: cfPath,
		Context:       ctxDir,
		Ref:           ref,
		Addr:          daemon.Addr(),
	}.Args(dest)
	if err != nil {
		return fmt.Errorf("EmitVmBox: %w", err)
	}
	if out, err := (buildkitdrive.ExecRunner{}).Run(ctx, buildkitdrive.BuildctlBinary, args...); err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("EmitVmBox: buildctl build -t %s: %w: %s", ref, err, msg)
	}
	// The tag re-application is LoadExport's duty, not the caller's: a load returns
	// success while landing the image UNTAGGED, so without it `ref` resolves to nothing.
	if err := buildkitdrive.LoadExport(ctx, buildkitdrive.ExecRunner{}, container.EngineBinary(engine), dest, ref); err != nil {
		return fmt.Errorf("EmitVmBox: bridging %s into the %s store: %w", ref, engine, err)
	}
	// The export is a full copy of the image. Keeping it would double the storage of every
	// VM box on a persistent root, where GC is not yet proven, so it is removed once the
	// bridge has succeeded — and a failure to remove it is reported, not swallowed.
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("EmitVmBox: removing the export %s: %w", dest, err)
	}
	return nil
}

// resolveVmBoxArtifacts makes every artefact's host path absolute and stats it, so a
// missing or unreadable artefact fails BEFORE anything is staged or built.
func resolveVmBoxArtifacts(arts []vmBoxArtifact) ([]vmBoxArtifact, error) {
	resolved := make([]vmBoxArtifact, 0, len(arts))

	for _, a := range arts {
		abs, err := filepath.Abs(a.src)
		if err != nil {
			return nil, fmt.Errorf("EmitVmBox: resolving artifact %q: %w", a.src, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("EmitVmBox: artifact %q: %w", abs, err)
		}
		resolved = append(resolved, vmBoxArtifact{src: abs, inImage: a.inImage})
	}

	return resolved, nil
}

// stageVmBoxArtifacts decides the build context and the COPY list. An artefact the box
// must not carry verbatim — a copy-on-write qcow2 overlay, whose BACKING FILE is a
// host-absolute path under the build cache, so the guest cannot open it on any other host
// (the CrashLoop this emitter exists to prevent) — is FLATTENED into stageDir, and the
// context is then stageDir. When nothing needed rewriting and the artefacts all share one
// directory, the context IS that directory and nothing is copied or linked: the behaviour
// the single-disk callers have always had.
func stageVmBoxArtifacts(stageDir string, arts []vmBoxArtifact) (string, []vmBoxCopy, error) {
	staged := make([]vmBoxArtifact, len(arts))
	rewritten := false

	for i, a := range arts {
		overlay, err := isQcow2Overlay(a.src)
		if err != nil {
			return "", nil, fmt.Errorf("EmitVmBox: inspecting artifact %q: %w", a.src, err)
		}
		if !overlay {
			staged[i] = a

			continue
		}

		dst := filepath.Join(stageDir, artefactStageName(i, a.src))
		if err := flattenQcow2(a.src, dst); err != nil {
			return "", nil, fmt.Errorf("EmitVmBox: flattening artifact %q: %w", a.src, err)
		}

		staged[i] = vmBoxArtifact{src: dst, inImage: a.inImage}
		rewritten = true
	}

	ctxDir := stageDir
	if !rewritten && sameDir(staged) {
		ctxDir = filepath.Dir(staged[0].src)
	} else {
		for i := range staged {
			if filepath.Dir(staged[i].src) == stageDir {
				continue // already inside the context (a flattened overlay)
			}

			dst := filepath.Join(stageDir, artefactStageName(i, staged[i].src))
			if err := linkOrCopy(staged[i].src, dst); err != nil {
				return "", nil, fmt.Errorf("EmitVmBox: staging artifact %q: %w", staged[i].src, err)
			}

			staged[i].src = dst
		}
	}

	copies := make([]vmBoxCopy, 0, len(staged))
	for _, a := range staged {
		copies = append(copies, vmBoxCopy{contextName: filepath.Base(a.src), inImage: a.inImage})
	}

	return ctxDir, copies, nil
}

// artefactStageName is the name an artefact takes inside the staging directory: its index
// plus its own basename, because two artefacts of one box may share a basename and a COPY
// source collision would silently deliver the same file twice.
func artefactStageName(index int, src string) string {
	return fmt.Sprintf("%d-%s", index, filepath.Base(src))
}

// sameDir reports whether every artefact lives in one directory.
func sameDir(arts []vmBoxArtifact) bool {
	if len(arts) == 0 {
		return true
	}

	dir := filepath.Dir(arts[0].src)
	for _, a := range arts[1:] {
		if filepath.Dir(a.src) != dir {
			return false
		}
	}

	return true
}

// linkOrCopy hardlinks src to dst, falling back to a byte copy ONLY when the filesystem
// refuses the link across the two paths (EXDEV — a rootfs on another mount). The fallback
// is what lets such a rootfs emit at all; it is the one case in which an artefact is copied.
func linkOrCopy(src, dst string) error {
	err := os.Link(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	return copyFile(src, dst)
}

// copyFile is the link fallback.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return err
	}

	return out.Close()
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

// repoLeaf returns the repository LEAF of an OCI ref for the ai.opencharly.box label —
// the retention-group value. It strips any tag (@digest / :tag) and registry/host +
// namespace path, so `localhost/charly-check-kubevirt-vm-box:2026.279.1211` → the repo
// leaf. NOTE (measured on the fix): `spec.LeafName` is NOT usable here — it splits on
// NAMESPACE DOTS, so a dotted CalVer tag yields the tag, not the repo
// (opencharly/charly#808).
func repoLeaf(ref string) string {
	// Drop any digest, then any tag.
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndexByte(ref, ':'); i >= 0 {
		// Only treat the last ':' as a tag separator when it is after the final '/'
		// (a registry host may carry a port, e.g. localhost:5000/repo).
		if j := strings.LastIndexByte(ref, '/'); i > j {
			ref = ref[:i]
		}
	}
	if i := strings.LastIndexByte(ref, '/'); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// renderVmBoxContainerfile is the PURE half of the emitter: the scratch image, the disk
// COPY at the requested in-image path, and the metadata LABELs. Split out so the
// in-image path contract (the VM-box /disk.qcow2 vs the KubeVirt containerDisk
// /disk/disk.img) is unit-testable with no container engine.
func renderVmBoxContainerfile(copies []vmBoxCopy, metaJSON []byte, meta *spec.VmBoxMetadata, boxLabel string) string {
	var cf strings.Builder
	cf.WriteString("# VM box image - generated by deploykit.EmitVmBox (do not edit)\n")
	cf.WriteString("FROM scratch\n")
	// Each COPY is one artefact layer: the disk for the single-file shapes, the root drive
	// AND the kernel for the microVM shape. Double-quote a source only when its name needs
	// it (Dockerfile COPY strips matching double quotes).
	for _, c := range copies {
		fmt.Fprintf(&cf, "COPY %s %s\n", copyQuote(c.contextName), c.inImage)
	}
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
