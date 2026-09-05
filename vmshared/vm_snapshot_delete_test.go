package vmshared

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDeleteSnapshot_CleansLibvirtWhenRegistryEntryAbsent reproduces the measured
// dual-state gap (RCA #8): the registry entry is gone but the libvirt snapshot
// metadata still exists, blocking domain teardown. delete must still invoke the
// libvirt-side cleanup and succeed idempotently.
func TestDeleteSnapshot_CleansLibvirtWhenRegistryEntryAbsent(t *testing.T) {
	root := t.TempDir()
	t.Setenv(VmStateDirEnv, root)
	vm := "test-vm"
	snap := "golden"

	// The measured state: a registry file WITHOUT the entry (disk removed / entry lost).
	reg := SnapshotRegistry{Version: 1, Snapshots: map[string]*SnapshotEntry{}}
	if err := saveRegistry(vm, &reg); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}

	called := false
	original := DeleteExternalSnapshot
	DeleteExternalSnapshot = func(vmName string, entry *SnapshotEntry) error {
		called = true
		if entry == nil || entry.LibvirtName != snap {
			t.Errorf("unexpected libvirt entry: %+v", entry)
		}
		return nil
	}
	defer func() { DeleteExternalSnapshot = original }()

	if err := DeleteSnapshot(SnapshotDeleteOpts{VmName: vm, SnapName: snap}); err != nil {
		t.Fatalf("DeleteSnapshot should succeed idempotently when only libvirt metadata remains: %v", err)
	}
	if !called {
		t.Fatal("DeleteExternalSnapshot must be invoked to clean the libvirt metadata")
	}
}

// TestDeleteSnapshot_SurfacesLibvirtFailure: if the libvirt-side cleanup itself
// fails while the entry is absent, the error must surface (never a silent success).
func TestDeleteSnapshot_SurfacesLibvirtFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv(VmStateDirEnv, root)
	vm := "test-vm"

	reg := SnapshotRegistry{Version: 1, Snapshots: map[string]*SnapshotEntry{}}
	if err := saveRegistry(vm, &reg); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}
	original := DeleteExternalSnapshot
	DeleteExternalSnapshot = func(string, *SnapshotEntry) error {
		return os.ErrPermission
	}
	defer func() { DeleteExternalSnapshot = original }()

	if err := DeleteSnapshot(SnapshotDeleteOpts{VmName: vm, SnapName: "golden"}); err == nil {
		t.Fatal("expected a surfaced error when libvirt cleanup fails")
	}
}

// TestDeleteSnapshot_RegistryEntryPresentStillWorks guards the existing happy path.
func TestDeleteSnapshot_RegistryEntryPresentStillWorks(t *testing.T) {
	root := t.TempDir()
	t.Setenv(VmStateDirEnv, root)
	vm := "test-vm"

	reg := SnapshotRegistry{Version: 1, Snapshots: map[string]*SnapshotEntry{
		"golden": {Name: "golden", Mode: "external"},
	}}
	if err := saveRegistry(vm, &reg); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}
	original := DeleteExternalSnapshot
	DeleteExternalSnapshot = func(string, *SnapshotEntry) error { return nil }
	defer func() { DeleteExternalSnapshot = original }()

	if err := DeleteSnapshot(SnapshotDeleteOpts{VmName: vm, SnapName: "golden"}); err != nil {
		t.Fatalf("DeleteSnapshot with a present entry: %v", err)
	}
	reg2, err := loadRegistry(vm)
	if err != nil {
		t.Fatalf("loadRegistry after delete: %v", err)
	}
	if _, ok := reg2.Snapshots["golden"]; ok {
		t.Fatal("the deleted snapshot must be gone from the registry")
	}
}

// TestCreateSnapshot_StaleRegistryEntryRecaptures covers the golden-refresh
// idempotency gap (RCA: the golden was deleted to force a re-capture, but the
// registry still held the entry — CreateSnapshot refused with "already exists"
// even though the disk was gone). A registry entry whose disk is MISSING is a
// stale record: re-capturing over it is the operator's intent, so the stale
// entry is dropped and the capture proceeds. A LIVE entry (disk present) is
// still a hard conflict.
func TestCreateSnapshot_StaleRegistryEntryRecaptures(t *testing.T) {
	root := t.TempDir()
	t.Setenv(VmStateDirEnv, root)
	vm := "test-vm"
	snap := "golden"

	// Stale state: a registry entry whose disk path points to a MISSING file.
	diskPath, err := snapshotExternalDiskPath(vm, snap)
	if err != nil {
		t.Fatalf("snapshotExternalDiskPath: %v", err)
	}
	reg := SnapshotRegistry{Version: 1, Snapshots: map[string]*SnapshotEntry{
		snap: {Name: snap, Mode: "external", LibvirtName: snap, DiskPath: diskPath, Refcount: 2},
	}}
	if err := saveRegistry(vm, &reg); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}

	// Stub the external capture so the test never touches libvirt.
	called := false
	original := CreateExternalSnapshot
	CreateExternalSnapshot = func(opts SnapshotCreateOpts, outFile string) error {
		called = true
		return os.WriteFile(outFile, []byte("captured"), 0o644)
	}
	defer func() { CreateExternalSnapshot = original }()

	entry, err := CreateSnapshot(SnapshotCreateOpts{VmName: vm, SnapName: snap, Mode: "external"})
	if err != nil {
		t.Fatalf("stale entry must re-capture, got: %v", err)
	}
	if !called {
		t.Fatal("CreateExternalSnapshot must be invoked for the re-capture")
	}
	if entry == nil || entry.Name != snap {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	// The registry now holds the fresh entry with the real disk.
	reg2, err := loadRegistry(vm)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	if e := reg2.Snapshots[snap]; e == nil || e.DiskPath != diskPath {
		t.Fatalf("registry entry not refreshed: %+v", e)
	}
}

// TestCreateSnapshot_LiveEntryStillConflicts: a registry entry whose disk EXISTS
// is a live snapshot — re-capturing over it must still be a hard conflict.
func TestCreateSnapshot_LiveEntryStillConflicts(t *testing.T) {
	root := t.TempDir()
	t.Setenv(VmStateDirEnv, root)
	vm := "test-vm"
	snap := "golden"

	diskPath, err := snapshotExternalDiskPath(vm, snap)
	if err != nil {
		t.Fatalf("snapshotExternalDiskPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diskPath, []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := SnapshotRegistry{Version: 1, Snapshots: map[string]*SnapshotEntry{
		snap: {Name: snap, Mode: "external", LibvirtName: snap, DiskPath: diskPath, Refcount: 1},
	}}
	if err := saveRegistry(vm, &reg); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}

	if _, err := CreateSnapshot(SnapshotCreateOpts{VmName: vm, SnapName: snap, Mode: "external"}); err == nil {
		t.Fatal("live entry must still conflict")
	}
}

// TestCreateSnapshot_StatErrorStillConflicts: a stat failure that is NOT
// not-exists (e.g. ENOTDIR — a path component is a file) must NOT delete a live
// entry. Only os.IsNotExist qualifies as a stale/missing disk; anything else
// falls through to the hard-conflict path (B18).
func TestCreateSnapshot_StatErrorStillConflicts(t *testing.T) {
	root := t.TempDir()
	t.Setenv(VmStateDirEnv, root)
	vm := "test-vm"
	snap := "golden"

	// A path whose parent component is a regular FILE: os.Stat returns ENOTDIR
	// (not os.IsNotExist) — the entry must still conflict, never be deleted.
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	diskPath := filepath.Join(blocker, "golden", "disk.qcow2")
	reg := SnapshotRegistry{Version: 1, Snapshots: map[string]*SnapshotEntry{
		snap: {Name: snap, Mode: "external", LibvirtName: snap, DiskPath: diskPath, Refcount: 1},
	}}
	if err := saveRegistry(vm, &reg); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}

	called := false
	original := CreateExternalSnapshot
	CreateExternalSnapshot = func(opts SnapshotCreateOpts, outFile string) error {
		called = true
		return nil
	}
	defer func() { CreateExternalSnapshot = original }()

	if _, err := CreateSnapshot(SnapshotCreateOpts{VmName: vm, SnapName: snap, Mode: "external"}); err == nil {
		t.Fatal("non-not-exists stat error must still conflict")
	}
	if called {
		t.Fatal("CreateExternalSnapshot must NOT be invoked for a non-not-exists stat error")
	}
	// The registry entry must survive (never deleted on a non-not-exists error).
	reg2, err := loadRegistry(vm)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	if reg2.Snapshots[snap] == nil {
		t.Fatal("registry entry must survive a non-not-exists stat error")
	}
}
