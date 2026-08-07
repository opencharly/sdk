package deploykit

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/opencharly/sdk/kit"
)

// deploy_config_cycle_test.go — the red/green pair for the per-host overlay's lost-update race.
//
// The RED arm reproduces the pre-fix shape every unlocked writer used (candy/plugin-deploy-pod's
// config-setup writes, candy/plugin-fleet's import/reset/ephemeral writes): read the overlay,
// mutate a snapshot, save it back. It is deterministic, not probabilistic — every writer completes
// its read before any writer saves, which is exactly what a minutes-long `charly config`
// orchestration does relative to a sibling bed's write. The GREEN arm runs the same writers through
// MutateFleetConfig and asserts every entry survives.

// fakeOverlayStore models the per-host overlay file for these tests: `state` is the bytes on disk
// and read/save are the reader/persist callbacks the cycle takes.
//
// The mutex makes each INDIVIDUAL read and save atomic — the guarantee a real filesystem gives for
// free (and what lets these tests run clean under -race, which cannot see happens-before through an
// OS flock). It deliberately does NOT span read→mutate→save: that whole-cycle atomicity is the
// property under test, supplied by the deploy-config flock in the green arm and absent by
// construction in the red one.
type fakeOverlayStore struct {
	mu    sync.Mutex
	state *FleetConfig
}

func (f *fakeOverlayStore) read() (*FleetConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state == nil {
		return nil, nil
	}
	// Hand back a copy, exactly as a real read does: a caller mutating its result must not be
	// silently mutating the stored config.
	cp := &FleetConfig{Fleet: make(map[string]FleetNode, len(f.state.Fleet))}
	for k, v := range f.state.Fleet {
		cp.Fleet[k] = v
	}
	return cp, nil
}

func (f *fakeOverlayStore) save(dc *FleetConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = dc
	return nil
}

// isolateDeployConfigPath points kit.DefaultDeployConfigPath at a fresh temp file so the flock
// MutateFleetConfig derives from it is a real, isolated OS lock — never the operator's own.
func isolateDeployConfigPath(t *testing.T) {
	t.Helper()
	t.Setenv(kit.DeployConfigEnv, filepath.Join(t.TempDir(), "charly.yml"))
}

// TestMutateFleetConfig_ConcurrentMutatorsOfDifferentEntriesAllSurvive is the GREEN arm: N
// concurrent writers, each adding its OWN entry, all survive. This is the property the pod
// config-setup path lost — a concurrent bed's resolved_image, arbiter claim, and whole entry were
// discarded by a sibling's stale write-back.
func TestMutateFleetConfig_ConcurrentMutatorsOfDifferentEntriesAllSurvive(t *testing.T) {
	isolateDeployConfigPath(t)
	store := &fakeOverlayStore{}

	const n = 12
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("bed-%02d", i)
			<-start // maximize contention: every writer enters the cycle at once
			_, errs[i] = MutateFleetConfig(store.read, store.save, func(dc *FleetConfig) (bool, error) {
				dc.Fleet[name] = FleetNode{Image: name, ResolvedImage: name + ":latest"}
				return true, nil
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("MutateFleetConfig() writer %d error = %v", i, err)
		}
	}
	if store.state == nil {
		t.Fatal("no config was written at all")
	}
	if len(store.state.Fleet) != n {
		t.Fatalf("overlay has %d entries after %d concurrent writers, want %d — a write was lost: %v", len(store.state.Fleet), n, n, store.state.Fleet)
	}
	for i := range n {
		name := fmt.Sprintf("bed-%02d", i)
		entry, ok := store.state.Fleet[name]
		if !ok {
			t.Errorf("entry %q missing — its writer's update was clobbered", name)
			continue
		}
		if entry.ResolvedImage != name+":latest" {
			t.Errorf("entry %q ResolvedImage = %q, want %q — the field a stale write-back silently reverted on the roster", name, entry.ResolvedImage, name+":latest")
		}
	}
}

// TestUnlockedReadModifySaveLosesUpdates is the RED arm: it drives the pre-fix shape — read,
// mutate the snapshot, save it back, with no lock and no re-read — and asserts it DOES lose
// updates. Deterministic: the barrier makes every writer finish its read before any writer saves,
// modelling the real window (a `charly config` run that loads the overlay, then spends minutes
// resolving ports and provisioning volumes before writing). Without this arm the green test above
// could pass on a build where the lock did nothing.
func TestUnlockedReadModifySaveLosesUpdates(t *testing.T) {
	isolateDeployConfigPath(t)
	store := &fakeOverlayStore{}

	const n = 12
	var readsDone sync.WaitGroup
	var wg sync.WaitGroup
	readsDone.Add(n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("bed-%02d", i)
			dc, err := store.read()
			if err != nil {
				t.Errorf("read: %v", err)
				return
			}
			dc = ensureFleetConfig(dc)
			readsDone.Done()
			readsDone.Wait() // every writer now holds the SAME pre-write snapshot
			dc.Fleet[name] = FleetNode{Image: name, ResolvedImage: name + ":latest"}
			if err := store.save(dc); err != nil {
				t.Errorf("save: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if store.state == nil {
		t.Fatal("no config was written at all")
	}
	if got := len(store.state.Fleet); got != 1 {
		t.Fatalf("unlocked read-modify-save kept %d of %d entries — this arm must demonstrate the lost update (exactly one survivor), or it is no longer exercising the pre-fix shape", got, n)
	}
}
