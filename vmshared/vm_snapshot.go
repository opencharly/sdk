package vmshared

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// vm_snapshot.go — declarative snapshot orchestration. Holds the
// per-VM unified registry.json (single source of truth), refcount
// management, and the mode-aware dispatch into vm_snapshot_libvirt.go
// (external mode) and vm_snapshot_internal.go (internal mode).
//
// Storage layout, per VM:
//
//   ~/.local/share/charly/vm/charly-<vm>/
//   ├── disk.qcow2                          # primary; also holds internal snapshots
//   └── snapshots/
//       ├── registry.json                   # ALL snapshots (internal + external)
//       └── <name>/                         # external mode only
//           ├── disk.qcow2
//           └── meta.json                   # description / created / parent / refcount
//
// The registry is the source of truth. The per-directory meta.json is a
// self-describing fallback so a manual disk inspection still makes sense
// if the registry desyncs.

// SnapshotRegistry is the on-disk schema for snapshots/registry.json.
// Versioned so future shape evolutions can migrate cleanly.
type SnapshotRegistry struct {
	// Version is the registry schema version. V1 is the initial release.
	Version int `json:"version"`

	// Snapshots is the unified set of snapshots known to charly for this
	// VM, keyed by Name. Both modes appear here.
	Snapshots map[string]*SnapshotEntry `json:"snapshots"`
}

// SnapshotEntry is one snapshot record. Mirrors VmSnapshotState plus
// on-disk-only fields (the registry is internal; VmSnapshotState is the
// charly.yml-facing mirror).
type SnapshotEntry struct {
	// Name uniquely identifies the snapshot within this VM.
	Name string `json:"name"`

	// Mode is "external" or "internal".
	Mode string `json:"mode"`

	// LibvirtName is the snapshot's name as known to libvirt. For
	// external mode, libvirt registers the snapshot as a domain
	// snapshot and we store the libvirt-side identifier here. For
	// internal mode, this matches Name (qemu-img embeds the literal
	// name).
	LibvirtName string `json:"libvirt_name,omitempty"`

	// DiskPath is the absolute path to the external snapshot file.
	// Empty for internal-mode snapshots.
	DiskPath string `json:"disk_path,omitempty"`

	// Description carries the operator-supplied note.
	Description string `json:"description,omitempty"`

	// Created is the RFC3339 creation timestamp.
	Created string `json:"created,omitempty"`

	// Parent is the prior snapshot in the implicit chain at create
	// time (whichever was current then). Informational; helps trace
	// backing-chain ancestry.
	Parent string `json:"parent,omitempty"`

	// Refcount tracks active clones / ephemerals depending on this
	// snapshot. delete refuses while > 0.
	Refcount int `json:"refcount"`

	// Quiesced records whether the snapshot was taken with guest-agent
	// fsfreeze active. Informational; helps an operator decide
	// whether the snapshot is consistent.
	Quiesced bool `json:"quiesced,omitempty"`
}

// snapshotsDir returns the absolute path to the snapshots/ directory
// for a given VM. Creates intermediate directories on demand.
func snapshotsDir(vmName string) (string, error) {
	base, err := VmStateRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "charly-"+vmName, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating snapshots dir %s: %w", dir, err)
	}
	return dir, nil
}

// vmDiskPath returns the absolute path to the VM's primary qcow2 disk
// (the file that holds internal snapshots and that external snapshots
// back onto). For charly-built VMs this is <vm.image_dir>/<vm>/disk.qcow2 in the
// project tree; for adopted (imported) VMs this is the path recorded in
// VmSource.DiskPath.
//
// V1 returns a best-effort guess: project-relative image path if it
// exists, otherwise empty. Callers that need authoritative resolution
// (for clone backing, for libvirt snapshot XML) should pass an
// explicit override; this helper is for the registry's own bookkeeping.
func vmDiskPath(vmName string) (string, error) {
	// Per-VM disk dir used by the charly vm build cloud_image / bootc / bootstrap
	// paths — the same <vm.image_dir>/<vm>/disk.qcow2 layout VmDiskDir derives.
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	diskDir, err := VmDiskDir(vmName)
	if err != nil {
		return "", err
	}
	// An ABSOLUTE vm.image_dir pins the root globally (VmImageDirEnv's documented
	// contract): use it as-is. filepath.Join does NOT reset on an absolute element
	// (Join("/home/u", "/srv/img") == "/home/u/srv/img"), so gluing cwd onto an
	// absolute root would stat a path that does not exist and break exactly the
	// relocation this feature exists to enable.
	if !filepath.IsAbs(diskDir) {
		diskDir = filepath.Join(cwd, diskDir)
	}
	candidate := filepath.Join(diskDir, "disk.qcow2")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	// Fall back to the VM state dir (some adoption flows symlink here).
	base, err := VmStateRoot()
	if err != nil {
		return "", err
	}
	candidate = filepath.Join(base, "charly-"+vmName, "disk.qcow2")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("vm %q: cannot locate primary disk (looked in %s/disk.qcow2 and %s/charly-%s/disk.qcow2)", vmName, diskDir, base, vmName)
}

// registryPath returns the registry.json path for a VM.
func registryPath(vmName string) (string, error) {
	dir, err := snapshotsDir(vmName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.json"), nil
}

// loadRegistry reads registry.json or returns an empty registry if the
// file doesn't exist. The empty-default behavior makes the first-snapshot
// flow a single write rather than two.
func loadRegistry(vmName string) (*SnapshotRegistry, error) {
	path, err := registryPath(vmName)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SnapshotRegistry{Version: 1, Snapshots: map[string]*SnapshotEntry{}}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var reg SnapshotRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if reg.Snapshots == nil {
		reg.Snapshots = map[string]*SnapshotEntry{}
	}
	if reg.Version == 0 {
		reg.Version = 1
	}
	return &reg, nil
}

// saveRegistry atomically writes the registry to disk (write-temp +
// rename pattern so a crash mid-write doesn't truncate the file).
func saveRegistry(vmName string, reg *SnapshotRegistry) error {
	path, err := registryPath(vmName)
	if err != nil {
		return err
	}
	if reg.Snapshots == nil {
		reg.Snapshots = map[string]*SnapshotEntry{}
	}
	if reg.Version == 0 {
		reg.Version = 1
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling registry: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming %s → %s: %w", tmp, path, err)
	}
	return nil
}

// snapshotMetaPath returns the per-snapshot meta.json sidecar path.
// External-mode only.
func snapshotMetaPath(vmName, snapName string) (string, error) {
	dir, err := snapshotsDir(vmName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, snapName, "meta.json"), nil
}

// snapshotExternalDiskPath returns the absolute path to the external
// snapshot's qcow2 file. Caller is expected to MkdirAll the parent
// before write.
func snapshotExternalDiskPath(vmName, snapName string) (string, error) {
	dir, err := snapshotsDir(vmName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, snapName, "disk.qcow2"), nil
}

// SnapshotBackingStalePath reports whether an external snapshot's backing chain
// has been REBUILT after the snapshot was captured, returning the offending
// backing file path. A non-empty return means the snapshot is STALE.
//
// Why a snapshot goes stale: the guest filesystem captured inside the snapshot
// (btrfs, ext4) references inodes in the BASE disks of its backing chain. When a
// later `vm build` rewrites one of those base disks (a re-provision, a disk-root
// change), the snapshot's superblock still points at the OLD data, so any clone
// backed by it boots into a broken/grub-rescue guest. The mtime ordering — a
// backing file newer than the snapshot's `Created` — is the detectable proxy.
//
// This is the ONE staleness definition (R3). The clone path (candy/plugin-vm)
// and the capture path (CreateSnapshot below) MUST agree on it: the clone guard
// already refuses a stale snapshot with an actionable error, so if capture did
// not also treat the same snapshot as stale it would refuse to refresh it
// (`already exists`), making the clone guard's promised recovery impossible.
//
// Second-precision comparison: the registry stores `Created` at RFC3339 second
// precision while a capture-finalization write can land sub-second later, so a
// same-second mtime is NOT stale (a false STALE would force needless re-captures).
func SnapshotBackingStalePath(entry *SnapshotEntry) (string, error) {
	if entry == nil || entry.DiskPath == "" || entry.Created == "" {
		return "", nil // nothing to check
	}
	created, err := time.Parse(time.RFC3339, entry.Created)
	if err != nil {
		return "", fmt.Errorf("parsing snapshot created time %q: %w", entry.Created, err)
	}
	cmd := exec.Command("qemu-img", "info", "--backing-chain", "-U", "--output", "json", entry.DiskPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("qemu-img info --backing-chain %s: %w", entry.DiskPath, err)
	}
	var chain []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(out, &chain); err != nil {
		return "", fmt.Errorf("parsing qemu-img backing chain: %w", err)
	}
	for _, img := range chain {
		if img.Filename == "" || img.Filename == entry.DiskPath {
			continue // the snapshot's own disk is not a backing file
		}
		fi, err := os.Stat(img.Filename)
		if err != nil {
			continue // a missing backing file is a different error (overlay create fails loudly)
		}
		if fi.ModTime().Truncate(time.Second).After(created.Truncate(time.Second)) {
			return img.Filename, nil
		}
	}
	return "", nil
}

// SnapshotCreateOpts parameterizes the creation of a snapshot.
type SnapshotCreateOpts struct {
	// VmName is the kind:vm entity name (without charly- prefix).
	VmName string

	// SnapName is the new snapshot's name.
	SnapName string

	// Mode is "external" or "internal" — empty defaults to external.
	Mode string

	// Description is an optional human note.
	Description string

	// Quiesce, when true, requests guest-agent fsfreeze before
	// snapshotting (with libvirt's plain freeze as fallback).
	Quiesce bool

	// LibvirtBackend, when non-nil, overrides the auto-detected backend.
	// Default: probe via the caller's own backend-resolve (candy/plugin-vm's
	// resolveVmBackendPlugin).
	LibvirtBackend string
}

// CreateSnapshot is the mode-aware orchestrator for `charly vm snapshot
// create`. Looks up the active VM, dispatches to the matching mode-
// specific implementation, and records the result in registry.json +
// meta.json.
func CreateSnapshot(opts SnapshotCreateOpts) (*SnapshotEntry, error) {
	if opts.VmName == "" {
		return nil, fmt.Errorf("CreateSnapshot: vm name is required")
	}
	if opts.SnapName == "" {
		return nil, fmt.Errorf("CreateSnapshot: snapshot name is required")
	}
	mode := opts.Mode
	if mode == "" {
		mode = "external"
	}
	if mode != "external" && mode != "internal" {
		return nil, fmt.Errorf("CreateSnapshot: unknown mode %q (want external or internal)", mode)
	}

	reg, err := loadRegistry(opts.VmName)
	if err != nil {
		return nil, err
	}
	if entry, exists := reg.Snapshots[opts.SnapName]; exists {
		// Golden-refresh idempotency — a snapshot may be re-captured in place when
		// it is STALE, and refused only when it is LIVE. "Stale" has TWO forms, and
		// both must be honoured (R3: one definition shared with the clone guard):
		//
		//   1. The disk is MISSING — the golden was deleted to force a re-capture,
		//      or a crashed run left the record behind. ONLY os.IsNotExist qualifies;
		//      any other stat failure (permission, transient I/O) must NOT delete a
		//      live entry — it falls through to the hard-conflict path.
		//   2. The disk is PRESENT but its backing chain was REBUILT after capture
		//      (SnapshotBackingStalePath). The clone guard refuses such a snapshot
		//      with "re-run the base bed to refresh it" — so capture MUST treat it as
		//      refreshable, or that promised recovery is impossible (the capture
		//      would answer `already exists` and bed_run would keep the stale golden
		//      forever). This is exactly the auto-recovery the guard's error names.
		//
		// A genuinely live external snapshot (disk present AND fresh) is still a hard
		// conflict.
		refreshable := false
		if entry.Mode == "external" && entry.DiskPath != "" {
			if _, serr := os.Stat(entry.DiskPath); serr != nil {
				if os.IsNotExist(serr) {
					refreshable = true // form 1: missing disk
				}
				// non-IsNotExist stat error falls through to the hard conflict
			} else {
				staleBacking, berr := SnapshotBackingStaleProbe(entry)
				// CONSERVATIVE on an indeterminate probe: a freshness check that
				// cannot run (a non-qcow2 disk, a transient qemu-img failure) must
				// NOT delete a possibly-live snapshot. Treat it as live (hard
				// conflict) and say why — the burden is on a PROVEN staleness.
				if berr != nil {
					fmt.Fprintf(os.Stderr, "note: snapshot %q on %q exists but its freshness could not be determined: %v — treating it as live\n",
						opts.SnapName, opts.VmName, berr)
				} else if staleBacking != "" {
					refreshable = true // form 2: stale backing chain
					fmt.Fprintf(os.Stderr, "note: snapshot %q on %q is STALE (backing %s was rebuilt after capture) — re-capturing over it\n",
						opts.SnapName, opts.VmName, staleBacking)
				}
			}
		}
		if !refreshable {
			return nil, fmt.Errorf("vm %q: snapshot %q already exists", opts.VmName, opts.SnapName)
		}
		// Clear BOTH stores the snapshot lives in before the re-create, for an
		// external snapshot:
		//
		//   1. LIBVIRT metadata — DomainSnapshotDelete (metadata-only). Left
		//      behind, the re-create fails ("snapshot already exists") and domain
		//      teardown is blocked ("cannot delete inactive domain with N
		//      snapshots"). Best-effort + idempotent: absent libvirt metadata (the
		//      crashed-run case) is not an error (DeleteExternalSnapshot tolerates
		//      a missing snapshot).
		//   2. The per-snapshot DIRECTORY on disk — libvirt's external-snapshot
		//      create REFUSES a target file that already exists ("external snapshot
		//      file for disk vda already exists and is not a block device"), and it
		//      OWNS creating that file. charly owns the disk lifecycle, so the
		//      stale disk.qcow2 (+ meta.json) is removed here, exactly as
		//      DeleteSnapshot does for a full delete. The dir is keyed by the same
		//      name the create below will write to.
		if entry.Mode == "external" {
			if derr := DeleteExternalSnapshot(opts.VmName, entry); derr != nil {
				return nil, fmt.Errorf("vm %q: clearing the stale snapshot %q from libvirt before re-capture: %w",
					opts.VmName, opts.SnapName, derr)
			}
			if dir, derr := snapshotsDir(opts.VmName); derr == nil {
				if rmerr := os.RemoveAll(filepath.Join(dir, opts.SnapName)); rmerr != nil {
					return nil, fmt.Errorf("vm %q: removing the stale snapshot %q dir before re-capture: %w",
						opts.VmName, opts.SnapName, rmerr)
				}
			}
		}
		delete(reg.Snapshots, opts.SnapName)
		if err := saveRegistry(opts.VmName, reg); err != nil {
			return nil, err
		}
	} else if mode == "external" {
		// No registry entry, but an ORPHANED snapshot dir may still exist on disk
		// (a crashed run, or a re-capture whose registry delete landed but whose
		// dir removal did not). libvirt's external-snapshot create REFUSES a target
		// file that already exists ("external snapshot file for disk vda already
		// exists and is not a block device"), and it OWNS creating that file — so
		// charly (the disk-lifecycle owner) clears a leftover dir before every
		// external create. Without this the very first re-capture after an
		// interrupted one can never succeed.
		if dir, derr := snapshotsDir(opts.VmName); derr == nil {
			orphan := filepath.Join(dir, opts.SnapName)
			if _, serr := os.Stat(orphan); serr == nil {
				fmt.Fprintf(os.Stderr, "note: clearing an orphaned snapshot dir %s (no registry entry) before capture\n", orphan)
				if rmerr := os.RemoveAll(orphan); rmerr != nil {
					return nil, fmt.Errorf("vm %q: removing the orphaned snapshot dir %s before capture: %w",
						opts.VmName, orphan, rmerr)
				}
			}
		}
	}

	// Implicit parent = whichever snapshot was most recently created
	// (the head of the chain). V1 picks the lexicographically last
	// matching mode; V2 will track an explicit "current" head.
	parent := implicitParent(reg)

	created := time.Now().UTC().Format(time.RFC3339)
	entry := &SnapshotEntry{
		Name:        opts.SnapName,
		Mode:        mode,
		LibvirtName: opts.SnapName,
		Description: opts.Description,
		Created:     created,
		Parent:      parent,
		Quiesced:    opts.Quiesce,
		Refcount:    0,
	}

	switch mode {
	case "external":
		diskPath, err := snapshotExternalDiskPath(opts.VmName, opts.SnapName)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
			return nil, fmt.Errorf("creating snapshot dir: %w", err)
		}
		if err := CreateExternalSnapshot(opts, diskPath); err != nil {
			return nil, fmt.Errorf("vm %q: external snapshot %q: %w", opts.VmName, opts.SnapName, err)
		}
		entry.DiskPath = diskPath
	case "internal":
		if err := CreateInternalSnapshot(opts); err != nil {
			return nil, fmt.Errorf("vm %q: internal snapshot %q: %w", opts.VmName, opts.SnapName, err)
		}
	}

	reg.Snapshots[opts.SnapName] = entry
	if err := saveRegistry(opts.VmName, reg); err != nil {
		return nil, err
	}
	if mode == "external" {
		if err := writeSnapshotMeta(opts.VmName, opts.SnapName, entry); err != nil {
			return nil, err
		}
	}
	return entry, nil
}

// ListSnapshots returns the snapshots for a VM as a name-sorted slice.
func ListSnapshots(vmName string) ([]*SnapshotEntry, error) {
	reg, err := loadRegistry(vmName)
	if err != nil {
		return nil, err
	}
	out := make([]*SnapshotEntry, 0, len(reg.Snapshots))
	for _, e := range reg.Snapshots {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SnapshotDeleteOpts parameterizes deletion.
type SnapshotDeleteOpts struct {
	VmName   string
	SnapName string
	// Force allows deletion even when refcount > 0. Default false.
	// Recommended only when the consuming clones/ephemerals have
	// already been destroyed and the registry is stale.
	Force bool
}

// DeleteSnapshot is the mode-aware deletion. Refuses while refcount > 0
// unless Force is set.
func DeleteSnapshot(opts SnapshotDeleteOpts) error {
	reg, err := loadRegistry(opts.VmName)
	if err != nil {
		return err
	}
	entry, ok := reg.Snapshots[opts.SnapName]
	if !ok {
		// Dual-state delete (measured gap, RCA #8): the registry entry can be
		// absent while the LIBVIRT snapshot metadata still exists (disk removed,
		// registry stale) — which blocks domain teardown with "cannot delete
		// inactive domain with 1 snapshots". Best-effort clean the libvirt side
		// so delete is idempotent and recovery always succeeds; any remaining
		// libvirt failure is surfaced.
		if err := DeleteExternalSnapshot(opts.VmName, &SnapshotEntry{Name: opts.SnapName, LibvirtName: opts.SnapName, Mode: "external"}); err != nil {
			return fmt.Errorf("vm %q: snapshot %q does not exist (libvirt cleanup failed): %w", opts.VmName, opts.SnapName, err)
		}
		delete(reg.Snapshots, opts.SnapName)
		_ = saveRegistry(opts.VmName, reg)
		return nil
	}
	if entry.Refcount > 0 && !opts.Force {
		return fmt.Errorf("vm %q: snapshot %q has refcount=%d (clones/ephemerals depend on it); pass --force only after destroying them",
			opts.VmName, opts.SnapName, entry.Refcount)
	}

	switch entry.Mode {
	case "external":
		if err := DeleteExternalSnapshot(opts.VmName, entry); err != nil {
			return fmt.Errorf("vm %q: external snapshot %q: %w", opts.VmName, opts.SnapName, err)
		}
		// Remove the per-snapshot directory + meta.json.
		dir, derr := snapshotsDir(opts.VmName)
		if derr == nil {
			_ = os.RemoveAll(filepath.Join(dir, opts.SnapName))
		}
	case "internal":
		if err := DeleteInternalSnapshot(opts.VmName, entry); err != nil {
			return fmt.Errorf("vm %q: internal snapshot %q: %w", opts.VmName, opts.SnapName, err)
		}
	default:
		return fmt.Errorf("vm %q: snapshot %q has unknown mode %q", opts.VmName, opts.SnapName, entry.Mode)
	}

	delete(reg.Snapshots, opts.SnapName)
	return saveRegistry(opts.VmName, reg)
}

// RevertSnapshot is the mode-aware revert.
func RevertSnapshot(vmName, snapName string) error {
	reg, err := loadRegistry(vmName)
	if err != nil {
		return err
	}
	entry, ok := reg.Snapshots[snapName]
	if !ok {
		return fmt.Errorf("vm %q: snapshot %q does not exist", vmName, snapName)
	}
	switch entry.Mode {
	case "external":
		return RevertExternalSnapshot(vmName, entry)
	case "internal":
		return RevertInternalSnapshot(vmName, entry)
	default:
		return fmt.Errorf("vm %q: snapshot %q has unknown mode %q", vmName, snapName, entry.Mode)
	}
}

// PromoteSnapshot converts an internal snapshot to external mode by
// extracting it via `qemu-img convert` to a new qcow2 file in the
// snapshots directory. After promotion, the snapshot is usable as a
// clone backing target. The internal snapshot inside the primary qcow2
// is left in place — promote is non-destructive.
func PromoteSnapshot(vmName, snapName string) (*SnapshotEntry, error) {
	reg, err := loadRegistry(vmName)
	if err != nil {
		return nil, err
	}
	entry, ok := reg.Snapshots[snapName]
	if !ok {
		return nil, fmt.Errorf("vm %q: snapshot %q does not exist", vmName, snapName)
	}
	if entry.Mode != "internal" {
		return nil, fmt.Errorf("vm %q: snapshot %q is already mode=%q (only internal snapshots are promotable)", vmName, snapName, entry.Mode)
	}

	diskPath, err := snapshotExternalDiskPath(vmName, snapName)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating snapshot dir: %w", err)
	}
	if err := PromoteInternalToExternal(vmName, entry, diskPath); err != nil {
		return nil, fmt.Errorf("vm %q: promoting snapshot %q: %w", vmName, snapName, err)
	}
	entry.Mode = "external"
	entry.DiskPath = diskPath
	if err := saveRegistry(vmName, reg); err != nil {
		return nil, err
	}
	if err := writeSnapshotMeta(vmName, snapName, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// IncrementSnapshotRefcount increases the refcount on the named
// snapshot. Used by clone/ephemeral instantiation paths.
func IncrementSnapshotRefcount(vmName, snapName string) error {
	reg, err := loadRegistry(vmName)
	if err != nil {
		return err
	}
	entry, ok := reg.Snapshots[snapName]
	if !ok {
		return fmt.Errorf("vm %q: snapshot %q does not exist (cannot reference)", vmName, snapName)
	}
	entry.Refcount++
	if err := saveRegistry(vmName, reg); err != nil {
		return err
	}
	if entry.Mode == "external" {
		_ = writeSnapshotMeta(vmName, snapName, entry)
	}
	return nil
}

// DecrementSnapshotRefcount decreases the refcount. Floors at 0.
func DecrementSnapshotRefcount(vmName, snapName string) error {
	reg, err := loadRegistry(vmName)
	if err != nil {
		return err
	}
	entry, ok := reg.Snapshots[snapName]
	if !ok {
		// Tolerant: a snapshot that's gone (manually removed) shouldn't
		// block ephemeral teardown. Log-and-continue.
		fmt.Fprintf(os.Stderr, "note: vm %q snapshot %q absent during refcount decrement (already deleted?)\n", vmName, snapName)
		return nil
	}
	if entry.Refcount > 0 {
		entry.Refcount--
	}
	if err := saveRegistry(vmName, reg); err != nil {
		return err
	}
	if entry.Mode == "external" {
		_ = writeSnapshotMeta(vmName, snapName, entry)
	}
	return nil
}

// LookupSnapshot returns a snapshot entry by name or an error.
func LookupSnapshot(vmName, snapName string) (*SnapshotEntry, error) {
	reg, err := loadRegistry(vmName)
	if err != nil {
		return nil, err
	}
	entry, ok := reg.Snapshots[snapName]
	if !ok {
		return nil, fmt.Errorf("vm %q: snapshot %q does not exist; create with: charly vm snapshot create %s %s", vmName, snapName, vmName, snapName)
	}
	return entry, nil
}

// implicitParent returns the most-recently-created snapshot name in the
// registry, or empty if there are none. Used for implicit chain
// tracking at create-time (V1 doesn't honor explicit From: yet).
func implicitParent(reg *SnapshotRegistry) string {
	var newest string
	var newestTime time.Time
	for name, e := range reg.Snapshots {
		t, err := time.Parse(time.RFC3339, e.Created)
		if err != nil {
			continue
		}
		if newest == "" || t.After(newestTime) {
			newest = name
			newestTime = t
		}
	}
	return newest
}

// writeSnapshotMeta emits the per-snapshot meta.json sidecar for
// external-mode snapshots. Internal-mode snapshots have no sidecar.
func writeSnapshotMeta(vmName, snapName string, entry *SnapshotEntry) error {
	if entry.Mode != "external" {
		return nil
	}
	path, err := snapshotMetaPath(vmName, snapName)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
