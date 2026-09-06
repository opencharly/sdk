package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestMaterializedCache_ConfigChangeReMaterializesAndUpdates is the user-facing contract: the
// cache DETECTS a config change (no manual invalidation) — a different project state re-materializes
// AND the new state's entry is written automatically, so a subsequent same-state load hits it.
func TestMaterializedCache_ConfigChangeReMaterializesAndUpdates(t *testing.T) {
	isolateCacheRoot(t)
	lp1, res := testMaterializedState()
	lp2 := lp1
	lp2.ID = 99 // a different project state (the walk assigns a fresh id per config)
	lp2.Docs = nil

	exec := &countingExecutor{canned: lp1, result: res}
	seams := LoadSeamsFromExecutor(exec)
	merged := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp1, merged, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("materialize state 1: %v", err)
	}
	if exec.materializeCalls != 1 {
		t.Fatalf("state 1 load must materialize once, got %d", exec.materializeCalls)
	}
	// State 2: a DIFFERENT config state — the cache must DETECT it and re-materialize.
	merged2 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp2, merged2, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("materialize state 2: %v", err)
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("state 2 (config change) must re-materialize (detected), got %d calls", exec.materializeCalls)
	}
	// State 2 AGAIN: now the NEW entry exists — the cache UPDATED automatically.
	merged3 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp2, merged3, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("materialize state 2 again: %v", err)
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("state 2 reload must hit the UPDATED entry (no third materialize), got %d calls", exec.materializeCalls)
	}
	// State 1 again: its entry is still there too.
	merged4 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp1, merged4, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("materialize state 1 again: %v", err)
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("state 1 reload must hit its entry too (no fourth materialize), got %d calls", exec.materializeCalls)
	}
}

// TestMaterializedCache_LoaderIdentityDriftReKeys is the Phase 3 RCA gate (the stale-loader-logic
// class): a NEW binary identity (loader-logic change without a schema bump) must produce a NEW key
// AND the read must refuse an entry written under the OLD identity (the spike's stale-tree failure).
func TestMaterializedCache_LoaderIdentityDriftReKeys(t *testing.T) {
	isolateCacheRoot(t)
	lp, res := testMaterializedState()

	old := loaderIdentityFn
	defer func() { loaderIdentityFn = old }()
	loaderIdentityFn = func() string { return "sdk@old-loader" }

	exec := &countingExecutor{canned: lp, result: res}
	seams := LoadSeamsFromExecutor(exec)
	merged := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("materialize under old identity: %v", err)
	}
	if exec.materializeCalls != 1 {
		t.Fatalf("first load must materialize, got %d", exec.materializeCalls)
	}
	// The SAME config + a NEW identity (the new binary): must re-materialize (a new key).
	loaderIdentityFn = func() string { return "sdk@new-loader" }
	merged2 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged2, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("materialize under new identity: %v", err)
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("identity drift must re-materialize (the stale-loader-logic class is dead), got %d calls", exec.materializeCalls)
	}
	// The new identity again: its entry is now cached.
	merged3 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged3, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("materialize under new identity again: %v", err)
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("the new-identity entry must be hit (no third materialize), got %d calls", exec.materializeCalls)
	}
}
