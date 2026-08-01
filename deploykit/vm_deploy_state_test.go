package deploykit

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// vm_deploy_state_test.go — ported from charly/vm_deploy_state_test.go (F6 vm-lifecycle move,
// coneB-vmlifecycle): SaveVmDeployState/RemoveVmDeployEntry moved here, so their coverage moves
// with them. deploykit's own test binary has no charly/LoadUnified available (DeployStateHost is a
// charly-process-only registration — see deploy_file.go's own header), so these tests stub
// LoadUnifiedBundleConfig with an in-memory *BundleConfig instead of a real per-host overlay file:
// the stub returns the SAME live object on every call, so in-place mutations "persist" across
// SaveVmDeployState/RemoveVmDeployEntry calls exactly like a real file would, without needing a
// real marshal/parse round-trip (that fidelity is already covered by TestSaveBundleConfig_RoundTrip
// + charly's own marshalDeployNode tests). The lock-serialization test uses a REAL
// kit.AcquireFileLock over a temp path — the same production primitive — so the concurrency
// property under test (the lock actually prevents the lost-update race) is genuine, not simulated.

// newFakeVmDeployStateHost registers an in-memory DeployStateHost stub and returns the
// acquireLock/save callback pair SaveVmDeployState/RemoveVmDeployEntry take, backed by a REAL
// kit.AcquireFileLock over a fresh temp path (so the lock-serialization tests exercise the genuine
// OS-level primitive, not a fake).
func newFakeVmDeployStateHost(t *testing.T) (acquireLock func() (func() error, error), save func(*BundleConfig) error) {
	t.Helper()
	state := &BundleConfig{Bundle: map[string]BundleNode{}}
	prev := DeployStateHost
	RegisterDeployStateHost(&StateHostMechanisms{
		LoadUnifiedBundleConfig: func(configDir string) (*BundleConfig, error) {
			return state, nil
		},
	})
	t.Cleanup(func() { DeployStateHost = prev })

	lockPath := filepath.Join(t.TempDir(), "charly.yml.lock")
	acquireLock = func() (func() error, error) {
		return kit.AcquireFileLock(lockPath, true)
	}
	save = func(dc *BundleConfig) error {
		state = dc
		return nil
	}
	return acquireLock, save
}

// TestSaveVmDeployState_ConcurrentWritersAllSurvive proves SaveVmDeployState's load→modify→save
// cycle is serialized through the injected acquireLock — without it, concurrent writers (parallel
// `charly vm create` persist-auto-port, or a vm-create racing a `charly bundle add vm:<name>`)
// load→modify→save the same config and silently drop each other's entry.
func TestSaveVmDeployState_ConcurrentWritersAllSurvive(t *testing.T) {
	acquireLock, save := newFakeVmDeployStateHost(t)

	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("vm:e%02d", i)
			errs[i] = SaveVmDeployState(name, "", &spec.VmDeployState{SshPort: 3000 + i, Backend: "auto"}, acquireLock, save, nil)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d failed: %v", i, err)
		}
	}

	dc, err := LoadBundleConfig()
	if err != nil {
		t.Fatalf("final load: %v", err)
	}
	if dc == nil || dc.Bundle == nil {
		t.Fatal("no config persisted")
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("vm:e%02d", i)
		entry, ok := dc.Bundle[name]
		if !ok {
			t.Errorf("entry %q was lost — concurrent write race (lock not serializing)", name)
			continue
		}
		if entry.VmState == nil || entry.VmState.SshPort != 3000+i {
			t.Errorf("entry %q has wrong vm_state: %+v", name, entry.VmState)
		}
	}
}

// A single write round-trips, and the lock is released afterward (a second blocking write
// completes rather than self-deadlocking) — guards the acquire/defer-release balance.
func TestSaveVmDeployState_LockReleasedBetweenCalls(t *testing.T) {
	acquireLock, save := newFakeVmDeployStateHost(t)

	if err := SaveVmDeployState("vm:one", "", &spec.VmDeployState{SshPort: 2201}, acquireLock, save, nil); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// If the first call leaked the lock, this blocking acquire inside the second call would hang
	// the test (a self-deadlock surfaces as a timeout, never a silent pass).
	if err := SaveVmDeployState("vm:two", "", &spec.VmDeployState{SshPort: 2202}, acquireLock, save, nil); err != nil {
		t.Fatalf("second write (lock not released?): %v", err)
	}
	dc, err := LoadBundleConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := dc.Bundle["vm:one"]; !ok {
		t.Error("vm:one lost across the second write")
	}
	if _, ok := dc.Bundle["vm:two"]; !ok {
		t.Error("vm:two not persisted")
	}
}

// TestRemoveVmDeployEntry_RemovesBundleKeyedBedEntry exercises the `vm:`-form From-scan in
// VmDeployEntryKeys: a kind:check VM bed (e.g. check-k3s-vm) writes its vm_state under the BUNDLE
// key (check-k3s-vm) cross-referencing the VM ENTITY (k3s-vm). The scan lets the DIRECT
// `charly vm destroy k3s-vm` path (which builds "vm:k3s-vm") still resolve the bundle-keyed entry
// via that cross-ref — an exact-key delete on "vm:k3s-vm" alone would miss it and leak it. The
// From-scan must not over-match an UNRELATED bundle (check-other-vm, From=other-vm).
func TestRemoveVmDeployEntry_RemovesBundleKeyedBedEntry(t *testing.T) {
	acquireLock, save := newFakeVmDeployStateHost(t)

	// Seed through the REAL write path under the bundle/bed key (dctx.Name) with the resolved VM
	// entity — exactly how the vm lifecycle hook PrepareVenue persists it.
	if err := SaveVmDeployState("check-k3s-vm", "k3s-vm", &spec.VmDeployState{SshPort: 40161, Backend: "auto"}, acquireLock, save, nil); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	// An UNRELATED VM bundle that must survive the k3s-vm teardown (no over-match).
	if err := SaveVmDeployState("check-other-vm", "other-vm", &spec.VmDeployState{SshPort: 40162, Backend: "auto"}, acquireLock, save, nil); err != nil {
		t.Fatalf("seed unrelated: %v", err)
	}

	dc, err := LoadBundleConfig()
	if err != nil {
		t.Fatalf("reload after seed: %v", err)
	}
	seeded, ok := dc.Bundle["check-k3s-vm"]
	if !ok {
		t.Fatal("seed did not write the check-k3s-vm bundle entry")
	}
	if seeded.From != "k3s-vm" {
		t.Fatalf("seed entry missing vm: cross-ref (teardown linkage): got %q", seeded.From)
	}

	// The DIRECT `charly vm destroy k3s-vm` path reaches RemoveVmDeployEntry with the prefixed
	// ENTITY form — NOT the bundle key the entry was written under. The From-scan bridges the gap.
	if err := RemoveVmDeployEntry("vm:k3s-vm", acquireLock, save, nil); err != nil {
		t.Fatalf("RemoveVmDeployEntry: %v", err)
	}

	got, err := LoadBundleConfig()
	if err != nil {
		t.Fatalf("reload after teardown: %v", err)
	}
	if got == nil || got.Bundle == nil {
		t.Fatal("config vanished entirely")
	}
	if _, leaked := got.Bundle["check-k3s-vm"]; leaked {
		t.Error("bundle-keyed bed entry check-k3s-vm leaked after teardown (key-mismatch bug)")
	}
	if _, survived := got.Bundle["check-other-vm"]; !survived {
		t.Error("unrelated bundle check-other-vm was wrongly removed (over-match)")
	}
}

// TestSaveVmDeployState_SelfHealsStaleDottedTwin is the end-to-end regression test: an overlay
// carrying a pre-fix poisoned dotted twin gets healed the next time the canonical domain is
// written, via a real SaveVmDeployState call.
func TestSaveVmDeployState_SelfHealsStaleDottedTwin(t *testing.T) {
	acquireLock, save := newFakeVmDeployStateHost(t)

	// Seed a pre-fix poisoned overlay: a dotted twin alongside (what will become) the canonical entry.
	if err := save(&BundleConfig{Bundle: map[string]BundleNode{
		"check-sidecar-pod.check-sidecar-pod-ephvm": {Target: "vm", VmState: &spec.VmDeployState{SshPort: 45551}},
	}}); err != nil {
		t.Fatalf("seeding pre-fix overlay: %v", err)
	}

	// The canonical write — matches candy/plugin-vm's hostConfigPersist("vm:"+domainID, ...) call shape.
	if err := SaveVmDeployState("vm:check-sidecar-pod-check-sidecar-pod-ephvm", "eval-vm", &spec.VmDeployState{SshPort: 33799}, acquireLock, save, nil); err != nil {
		t.Fatalf("canonical write: %v", err)
	}

	dc, err := LoadBundleConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, stillPoisoned := dc.Bundle["check-sidecar-pod.check-sidecar-pod-ephvm"]; stillPoisoned {
		t.Error("the stale dotted twin survived the canonical write — self-heal did not fire")
	}
	entry, ok := dc.Bundle["vm:check-sidecar-pod-check-sidecar-pod-ephvm"]
	if !ok || entry.VmState == nil || entry.VmState.SshPort != 33799 {
		t.Errorf("canonical entry missing or wrong after self-heal: %+v", entry)
	}
}

// TestSaveVmDeployState_PreservesEphemeralOnSubsequentWrite is the regression test for the
// FINAL/K5 unit 6a RCA #7 live-probe-caught bug: registerEphemeralIfMarked (candy/plugin-bundle's
// ephemeral family) persists .VmState.Ephemeral under the canonical "vm:"+domainID key BEFORE
// `charly vm create`'s own state writes (e.g. the port_auto persist) run — RCA #6's key
// unification made this the COMMON ordering (the two writers never collided on separate keys
// before). A naive wholesale `entry.VmState = state` would silently ERASE the just-registered
// Ephemeral block, since the vm-create caller's state is never told about ephemeral registration.
func TestSaveVmDeployState_PreservesEphemeralOnSubsequentWrite(t *testing.T) {
	acquireLock, save := newFakeVmDeployStateHost(t)

	const key = "vm:check-sidecar-pod-check-sidecar-pod-ephvm"

	// Step 1: the ephemeral registration write (mirrors persistEphemeralRuntime — seeds
	// Target/From + an Ephemeral block, the FIRST write to a fresh overlay).
	if err := save(&BundleConfig{Bundle: map[string]BundleNode{
		key: {
			Target: "vm",
			From:   "eval-vm",
			VmState: &spec.VmDeployState{
				Ephemeral: &spec.EphemeralRuntime{ID: "abc123", Status: "active", DeployAddress: "check-sidecar-pod.check-sidecar-pod-ephvm"},
			},
		},
	}}); err != nil {
		t.Fatalf("seeding ephemeral-registered overlay: %v", err)
	}

	// Step 2: `charly vm create`'s own state write — the SAME key, a state that knows NOTHING
	// about the ephemeral block (this is the exact shape vm_create_orchestrate.go constructs).
	if err := SaveVmDeployState(key, "eval-vm", &spec.VmDeployState{SshPort: 41897, Backend: "auto"}, acquireLock, save, nil); err != nil {
		t.Fatalf("vm-create state write: %v", err)
	}

	dc, err := LoadBundleConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	entry, ok := dc.Bundle[key]
	if !ok {
		t.Fatal("canonical entry vanished")
	}
	if entry.VmState == nil {
		t.Fatal("VmState vanished entirely")
	}
	if entry.VmState.SshPort != 41897 {
		t.Errorf("SshPort = %d, want 41897 (the vm-create write's own field)", entry.VmState.SshPort)
	}
	if entry.VmState.Ephemeral == nil {
		t.Fatal("Ephemeral block was ERASED by the subsequent vm-create write — the RCA #7 bug")
	}
	if entry.VmState.Ephemeral.ID != "abc123" {
		t.Errorf("Ephemeral.ID = %q, want \"abc123\" (preserved from the register-time write)", entry.VmState.Ephemeral.ID)
	}
}

// TestSaveVmDeployState_ReverseOrderingRoundTrips double-checks the REVERSE ordering (vm-create's
// state write lands FIRST, ephemeral registration SECOND) still round-trips correctly —
// persistEphemeralRuntime's own logic (candy/plugin-bundle/ephemeral.go) reads the EXISTING
// node.VmState and only sets .Ephemeral on it, never wholesale-replacing, so this direction was
// never at risk — this test documents and locks that in from the SaveVmDeployState side.
func TestSaveVmDeployState_ReverseOrderingRoundTrips(t *testing.T) {
	acquireLock, save := newFakeVmDeployStateHost(t)

	const key = "vm:reverse-order-vm"

	// vm-create writes FIRST — no ephemeral knowledge yet.
	if err := SaveVmDeployState(key, "eval-vm", &spec.VmDeployState{SshPort: 50001}, acquireLock, save, nil); err != nil {
		t.Fatalf("vm-create state write: %v", err)
	}
	// A SECOND SaveVmDeployState call carrying an Ephemeral block (mirrors what
	// persistEphemeralRuntime effectively produces when it runs after vm-create: it reads the
	// EXISTING entry, so the passed-in state already contains the merged prior fields).
	if err := SaveVmDeployState(key, "eval-vm", &spec.VmDeployState{SshPort: 50001, Ephemeral: &spec.EphemeralRuntime{ID: "xyz789", Status: "active"}}, acquireLock, save, nil); err != nil {
		t.Fatalf("ephemeral-carrying write: %v", err)
	}

	dc, err := LoadBundleConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	entry, ok := dc.Bundle[key]
	if !ok || entry.VmState == nil {
		t.Fatal("canonical entry missing")
	}
	if entry.VmState.SshPort != 50001 {
		t.Errorf("SshPort = %d, want 50001", entry.VmState.SshPort)
	}
	if entry.VmState.Ephemeral == nil || entry.VmState.Ephemeral.ID != "xyz789" {
		t.Errorf("Ephemeral = %+v, want ID=xyz789", entry.VmState.Ephemeral)
	}
}

// TestSplitVmAddress_LedgerIdentityRegression is the regression test for the FINAL/K5 unit 6a
// RCA #9 live-probe-caught bug: `bundle del vm:<dotted-name>` silently no-op'd because
// ComputeDeployID hashes the deploy name VERBATIM, and the raw "vm:"-prefixed CLI form hashed to a
// COMPLETELY DIFFERENT ID than the plain form the add-time tree walk used to record the ledger
// entry (verified live: 6413f8070aaa6087 vs d81fff596411fea4 for the exact same logical
// deployment). Proves vmshared.SplitVmAddress's stripped form produces the IDENTICAL deployID as
// the plain add-time form — the fix hostBuildDeployNodeDelDispatch relies on. Lives here (moved
// from charly/vm_deploy_state_test.go) rather than sdk/vmshared (SplitVmAddress's own package)
// because it exercises the ComputeDeployID interaction specifically — vmshared cannot import
// deploykit (deploykit already imports vmshared; the reverse would cycle).
func TestSplitVmAddress_LedgerIdentityRegression(t *testing.T) {
	const addTimeName = "check-sidecar-pod.check-sidecar-pod-ephvm"
	const delTimeAddress = "vm:check-sidecar-pod.check-sidecar-pod-ephvm"

	addID := ComputeDeployID(addTimeName, nil, nil)

	// The BUG, preserved as a documented negative case: computing the ID from the raw,
	// unstripped CLI address produces a DIFFERENT id than the ledger was recorded under.
	buggyDelID := ComputeDeployID(delTimeAddress, nil, nil)
	if buggyDelID == addID {
		t.Fatalf("test assumption broken: the raw prefixed form no longer collides differently (got %q == %q) — re-verify ComputeDeployID's contract before trusting this regression test", buggyDelID, addID)
	}

	// THE FIX: strip via vmshared.SplitVmAddress before computing the ID — must match the
	// add-time ID.
	plain, isVm := vmshared.SplitVmAddress(delTimeAddress)
	if !isVm {
		t.Fatal("vmshared.SplitVmAddress did not recognize the vm: prefix")
	}
	fixedDelID := ComputeDeployID(plain, nil, nil)
	if fixedDelID != addID {
		t.Errorf("ComputeDeployID(vmshared.SplitVmAddress(%q)) = %q, want %q (the add-time ID) — the ledger record would still be unreachable from the del path", delTimeAddress, fixedDelID, addID)
	}
}
