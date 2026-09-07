package kit

import (
	"testing"
	"time"
)

// The fleet-del self-deadlock regression (RCA 2026-09-07): the ledger TRANSACTION lock
// (AcquireLedgerLock — held for a WHOLE fleet del/add, minutes, network fetches included) and
// the per-host charly.yml read-modify-write lock (paths.LockFile — the brief exclusion shared by
// writeLedger, deploykit.MutateFleetConfig/SaveFleetConfig and spec/refs GitClient.save) were
// collapsed onto ONE file (charly.yml.lock). flock is per-open-file-description, so two acquires
// of one path in ONE process conflict: every in-process nested config RMW under a transaction
// hold contended with the holder's own fd and stalled the full lockTimeout, silently dropping
// the write — 'charly fleet del <name> --assume-yes' wedged 7/7 (goroutine parked in
// spec/lock.flockBounded <- AcquireFileLock <- refs.GitClient.save, held under the del's own
// ledger-lock fd; kernel fdinfo showed one fd holding FLOCK WRITE on charly.yml.lock while the
// waiter's second fd retried forever).

// TestTxLockDoesNotSelfConflictWithConfigLock pins the fix's core property: holding the ledger
// TRANSACTION lock must NOT make a same-process acquire of the config RMW lock busy-fail — that
// busy-fail is exactly the wedge precondition (the blocking form parked the del forever and each
// timeout silently dropped a config/ledger write). Pre-fix this test fails: both locks were one
// file, so the second (non-blocking) acquire returned ErrLockBusy.
func TestTxLockDoesNotSelfConflictWithConfigLock(t *testing.T) {
	paths := writeLedgerFixture(t, "version: \"1\"\n")
	txLock, err := AcquireLedgerLock(paths)
	if err != nil {
		t.Fatalf("acquire transaction lock: %v", err)
	}
	defer func() { _ = txLock.Release() }()

	release, err := AcquireFileLock(paths.LockFile, false)
	if err != nil {
		t.Fatalf("config RMW lock busy while ONLY the transaction lock is held — the fleet-del self-deadlock precondition is back: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release config lock: %v", err)
	}
}

// TestLedgerTxLockPath_IsDedicated pins the path shape: the transaction lock file is its OWN
// file, never the config RMW lock path (see the deadlock regression above).
func TestLedgerTxLockPath_IsDedicated(t *testing.T) {
	paths := writeLedgerFixture(t, "version: \"1\"\n")
	tx := LedgerTxLockPath(paths)
	if tx == paths.LockFile {
		t.Fatalf("transaction lock path %q must differ from the config RMW lock path", tx)
	}
	if want := paths.ConfigFile + ".ledger.lock"; tx != want {
		t.Fatalf("transaction lock path = %q, want %q", tx, want)
	}
}

// TestAcquireLedgerLock_ExcludesPeers pins that the dedicated file did not dilute the
// transaction lock's OWN mutual exclusion: a second AcquireLedgerLock must not complete while
// the first is held, and must complete promptly after Release.
func TestAcquireLedgerLock_ExcludesPeers(t *testing.T) {
	paths := writeLedgerFixture(t, "version: \"1\"\n")
	first, err := AcquireLedgerLock(paths)
	if err != nil {
		t.Fatalf("acquire first transaction lock: %v", err)
	}

	acquired := make(chan *LedgerLock, 1)
	go func() {
		second, aerr := AcquireLedgerLock(paths)
		if aerr != nil {
			t.Errorf("second acquire: %v", aerr)
			acquired <- nil
			return
		}
		acquired <- second
	}()

	select {
	case second := <-acquired:
		if second != nil {
			t.Fatal("second AcquireLedgerLock completed while the first was held — transaction exclusion lost")
		}
		return
	case <-time.After(300 * time.Millisecond):
		// still blocked — correct
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release first: %v", err)
	}
	select {
	case second := <-acquired:
		if second == nil {
			t.Fatal("peer acquire errored")
		}
		_ = second.Release()
	case <-time.After(5 * time.Second):
		t.Fatal("peer acquire did not complete after Release")
	}
}
