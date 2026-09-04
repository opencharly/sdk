package vmshared

import (
	"os"
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
