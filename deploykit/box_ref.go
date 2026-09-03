package deploykit

// box_ref.go — the generic BoxRef resolver (VM-box cutover plan §2.2, task 4):
// ONE resolver over every VM source form, so no caller re-implements per-kind
// resolution (R3 — the source arms keep their schema shape; this is the generic
// engine underneath). It accepts the four forms
//
//	box:<candy-image>       → bootc install   (what source.box: drives today)
//	vm:<entity>@<snapshot>  → clone           (the snapshot's external disk)
//	image:<registry-ref>    → cloud_image     (artifact fetch + verify)
//	imported:<domain>       → adopt           (the entity's disk_path, resolved at build)
//
// and a bare name, which defaults to the box: form (matching how source.box:
// behaves today: plugin-vm's resolveBootcImageRef and sdk/kit's
// ResolveLocalImageRef accept the same bare-name / full-OCI-ref vocabulary).
//
// The resolver is a thin dispatcher over the arms' EXISTING primitives — the
// bootc local-image resolution plugin-vm's resolveBootcImageRef performs
// (vm_build_resolve.go: full-ref passthrough on "/", short names via
// kit.ResolveLocalImageRef), the snapshot-registry lookup BuildClone's
// parent-disk pick uses (vmshared.LookupSnapshot), and pass-throughs for the
// cloud_image / imported arms (their refs are consumed at fetch/build time).
// ctx / ex are reserved for the executor-mediated seams of later cutover wiring
// (remote host runtime config, the InvokeProvider auto-build fallback); every
// arm implemented today resolves from pure host state (container storage, the
// snapshot registry), so the resolver is unit-testable with no executor.

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// BoxRef form constants — the leading vocabulary of a BoxRef string.
const (
	BoxRefFormBox      = "box"
	BoxRefFormVm       = "vm"
	BoxRefFormImage    = "image"
	BoxRefFormImported = "imported"
)

// BoxRef SourceKind constants — the resolved side reuses the EXISTING VmSource
// kind vocabulary (plan §1.1 arms), so a ResolvedBoxRef can drive the same arms
// a vm.yml source block does.
const (
	BoxSourceBootc      = "bootc"
	BoxSourceClone      = "clone"
	BoxSourceCloudImage = "cloud_image"
	BoxSourceImported   = "imported"
)

// BoxRef is the parsed shape of a box reference string. Exactly one field is
// populated per form: Name (box: candy image, vm: entity, imported: domain),
// Snapshot (vm: @snapshot), URL (image:).
type BoxRef struct {
	Form     string
	Name     string
	Snapshot string
	URL      string
}

// ParseBoxRef parses a box reference into its form + payload. Known leading
// prefixes (box:, vm:, image:, imported:) route to their form; anything else is
// a bare name and defaults to the box: (container-image) form — the same
// default source.box: applies today. Malformed refs (empty, or a known prefix
// with an empty payload) error.
func ParseBoxRef(ref string) (*BoxRef, error) {
	s := strings.TrimSpace(ref)
	if s == "" {
		return nil, fmt.Errorf("box ref: empty reference")
	}
	switch {
	case strings.HasPrefix(s, BoxRefFormBox+":"):
		name := strings.TrimPrefix(s, BoxRefFormBox+":")
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("box ref %q: box: form needs a name", s)
		}
		return &BoxRef{Form: BoxRefFormBox, Name: name}, nil
	case strings.HasPrefix(s, BoxRefFormVm+":"):
		rest := strings.TrimPrefix(s, BoxRefFormVm+":")
		entity, snap, hasSnap := strings.Cut(rest, "@")
		if strings.TrimSpace(entity) == "" {
			return nil, fmt.Errorf("box ref %q: vm: form needs an entity (vm:<entity>[@<snapshot>])", s)
		}
		if hasSnap && strings.TrimSpace(snap) == "" {
			return nil, fmt.Errorf("box ref %q: vm: form has a trailing @ with an empty snapshot", s)
		}
		return &BoxRef{Form: BoxRefFormVm, Name: entity, Snapshot: snap}, nil
	case strings.HasPrefix(s, BoxRefFormImage+":"):
		url := strings.TrimPrefix(s, BoxRefFormImage+":")
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("box ref %q: image: form needs a url", s)
		}
		return &BoxRef{Form: BoxRefFormImage, URL: url}, nil
	case strings.HasPrefix(s, BoxRefFormImported+":"):
		domain := strings.TrimPrefix(s, BoxRefFormImported+":")
		if strings.TrimSpace(domain) == "" {
			return nil, fmt.Errorf("box ref %q: imported: form needs a domain", s)
		}
		return &BoxRef{Form: BoxRefFormImported, Name: domain}, nil
	default:
		// Bare name → the container-image form (source.box: default).
		return &BoxRef{Form: BoxRefFormBox, Name: s}, nil
	}
}

// ResolvedBoxRef is the outcome of resolving one BoxRef: the concrete VM source
// kind the ref names plus the concrete disk source that kind's build arm
// consumes.
//
// Named ResolvedBoxRef (not ResolvedBox) because deploykit already aliases
// ResolvedBox → buildkit.ResolvedBox (buildkit_aliases.go) for the pod/box
// BUILD-graph result — a different domain that cannot share the name in this
// package. The fields below ARE the plan §2.2 ResolvedBox{diskSource, metadata}
// contract: DiskSource is the concrete disk source, Metadata is best-effort box
// metadata (nil today — the distro-from-box read is a later cutover task).
type ResolvedBoxRef struct {
	SourceKind string              // one of the BoxSource* constants (bootc | clone | cloud_image | imported)
	DiskSource string              // image ref (bootc) / snapshot disk path (clone) / url (cloud_image) / domain (imported)
	Metadata   *spec.VmBoxMetadata // best-effort box metadata; nil until the metadata read lands
}

// ResolveBoxRef parses ref and resolves it to its source kind + concrete disk
// source, dispatching on the parsed form:
//
//   - box:        → bootc: a full ref (contains "/") passes through untouched —
//     bootc may pull it from a registry, so it is neither rewritten nor
//     required to exist locally; a short name (an internal kind:image candy)
//     resolves to its newest local CalVer tag via kit.ResolveLocalImageRef
//     (mirror of plugin-vm's resolveBootcImageRef).
//   - vm:@snap    → clone: the snapshot's external disk path via
//     vmshared.LookupSnapshot; an internal-mode snapshot (no external disk) is
//     refused — clone needs a disk to overlay.
//   - image:      → cloud_image: the url, passed through (fetch happens at
//     build time).
//   - imported:   → imported: the domain name, passed through (the entity's
//     disk_path is resolved at build time).
//
// Metadata is nil for every arm in this task. ctx / ex are not yet consulted by
// any arm (see the file header); pass context.Background() / nil in tests.
func ResolveBoxRef(ctx context.Context, ex *sdk.Executor, ref string) (*ResolvedBoxRef, error) {
	parsed, err := ParseBoxRef(ref)
	if err != nil {
		return nil, err
	}
	switch parsed.Form {
	case BoxRefFormBox:
		diskSource, err := resolveBootcDiskSource(parsed.Name)
		if err != nil {
			return nil, fmt.Errorf("box ref %q: %w", ref, err)
		}
		return &ResolvedBoxRef{SourceKind: BoxSourceBootc, DiskSource: diskSource}, nil
	case BoxRefFormVm:
		if parsed.Snapshot == "" {
			return nil, fmt.Errorf("box ref %q: vm: clone form requires a snapshot: vm:<entity>@<snapshot>", ref)
		}
		entry, err := vmshared.LookupSnapshot(parsed.Name, parsed.Snapshot)
		if err != nil {
			return nil, fmt.Errorf("box ref %q: %w", ref, err)
		}
		if entry.DiskPath == "" {
			return nil, fmt.Errorf("box ref %q: vm %q snapshot %q has no external disk path (mode %q) — clone needs an external snapshot", ref, parsed.Name, parsed.Snapshot, entry.Mode)
		}
		return &ResolvedBoxRef{SourceKind: BoxSourceClone, DiskSource: entry.DiskPath}, nil
	case BoxRefFormImage:
		return &ResolvedBoxRef{SourceKind: BoxSourceCloudImage, DiskSource: parsed.URL}, nil
	case BoxRefFormImported:
		return &ResolvedBoxRef{SourceKind: BoxSourceImported, DiskSource: parsed.Name}, nil
	}
	return nil, fmt.Errorf("box ref %q: unsupported form %q", ref, parsed.Form)
}

// resolveBootcDiskSource mirrors plugin-vm's resolveBootcImageRef
// (candy/plugin-vm/vm_build_resolve.go): a ref containing "/" is a full OCI ref
// and passes through untouched (bootc may pull it from a registry — no local
// existence requirement); a short name resolves to its newest local CalVer tag
// via kit.ResolveLocalImageRef — charly is CalVer-only, never ":latest".
func resolveBootcDiskSource(image string) (string, error) {
	if strings.Contains(image, "/") {
		return image, nil
	}
	resolved, err := kit.ResolveLocalImageRef(resolveEngine(), image)
	if err != nil {
		return "", fmt.Errorf("resolving bootc image %q: %w (build it first with `charly box build %s`)", image, err, image)
	}
	return resolved, nil
}

// resolveEngine picks the container engine binary for the bootc arm, mirroring
// plugin-vm's resolveVmBuild (vm_build_resolve.go): the host runtime config's
// engine.run when it resolves, podman otherwise. Unlike plugin-vm's build prep
// (which aborts on a corrupt runtime config), the resolver falls back to podman
// silently: the other three arms never consult the engine and podman IS the
// bootc default, so a config hiccup must not fail a resolution the caller could
// otherwise complete.
func resolveEngine() string {
	rt, err := kit.ResolveRuntime()
	if err != nil || rt == nil || rt.RunEngine == "" {
		return "podman"
	}
	return kit.EngineBinary(rt.RunEngine)
}
