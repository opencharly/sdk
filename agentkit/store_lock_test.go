package agentkit

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestFlockBounded_FailsFastOnContendedLock is the regression guard for the
// fleet-del stall: a store lock held by another process must not hang the
// caller forever — flockBounded fails fast after storeLockTimeout.
func TestFlockBounded_FailsFastOnContendedLock(t *testing.T) {
	old := storeLockTimeout
	storeLockTimeout = 200 * time.Millisecond
	defer func() { storeLockTimeout = old }()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Hold the lock exclusively on a SEPARATE fd — the contended shape
	// (flock on the same fd is idempotent).
	f2, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	if err := syscall.Flock(int(f2.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err = flockBounded(f)
	if err == nil {
		t.Fatal("flockBounded on a contended lock: expected an error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("flockBounded on a contended lock: took %s (unbounded hang)", elapsed)
	}
}
