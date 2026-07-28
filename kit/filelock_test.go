package kit

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// filelock_test.go — ported from charly/filelock_test.go (#118, P15 host-seam classification): the
// charly-side acquireFileLock/errLockBusy were pure pass-through aliases of AcquireFileLock/
// ErrLockBusy with zero charly-specific behavior, so they (and their coverage) moved here — sdk/kit
// had no dedicated test of AcquireFileLock's actual locking semantics before this move (only an
// incidental use inside http_fetch_race_test.go's own unrelated race coverage).

// A non-blocking acquire of an already-held lock must report ErrLockBusy, and
// the lock must be re-acquirable once released — the duplicate-run guard.
func TestAcquireFileLock_NonBlockingBusyThenReusable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")

	rel1, err := AcquireFileLock(path, false)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := AcquireFileLock(path, false); !errors.Is(err, ErrLockBusy) {
		t.Fatalf("second acquire while held: want ErrLockBusy, got %v", err)
	}
	if err := rel1(); err != nil {
		t.Fatalf("release: %v", err)
	}
	rel2, err := AcquireFileLock(path, false)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	if err := rel2(); err != nil {
		t.Fatalf("release after re-acquire: %v", err)
	}
}

// A blocking acquire must WAIT for the current holder to release rather than
// failing — and it must not proceed before the release. Deterministic without a
// sleep: the child blocks in flock and can only proceed after rel1() runs, by
// which point `released` is already set.
func TestAcquireFileLock_BlockingWaitsForRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "y.lock")

	rel1, err := AcquireFileLock(path, true)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	var released atomic.Bool
	got := make(chan error, 1)
	go func() {
		rel2, err := AcquireFileLock(path, true) // blocks until rel1() runs
		if err != nil {
			got <- err
			return
		}
		defer func() { _ = rel2() }()
		if !released.Load() {
			got <- errors.New("blocking acquire returned before the holder released")
			return
		}
		got <- nil
	}()

	released.Store(true)
	if err := rel1(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := <-got; err != nil {
		t.Fatal(err)
	}
}
