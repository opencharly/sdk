package buildcache

// lock.go — the per-key build lock, which lives under the RUNTIME root and NOT in the store tree.
//
// THE HOMES RULE, and why this file exists at all. sdk/storekit documents that a store tree
// contains NO locks and guards it with a test that walks a full store lifecycle (put, read, link,
// tree, entry, materialize) and fails if any ".lock" or "locks/" artifact exists under it. The
// reason is not tidiness: a lock has no content address, and a DIGEST-KEYED lock could even be
// shared between two processes that must not share it. A lock's home is therefore the STATE root
// (host-global arbitration) or the RUNTIME root (per-boot).
//
// This replaces the pre-cutover arrangement, where the lock lived INSIDE the cache tree as
// "<built binary path>.lock" (charly/charly/plugin_loader.go:240) — a cache entry's sibling, which
// is the forbidden shape. The lock moves here; the cache tree keeps only content.
//
// The lock IMPLEMENTATION is not this package's to own either: it already exists as the shared
// lock primitive, and this package only chooses WHERE the lock file lives (its root) and HOW it is
// keyed. Growing a second locking convention here would be the same R2 mistake the package exists
// to remove.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/opencharly/sdk/storekit"
	"github.com/opencharly/spec/lock"
)

// buildLockWait bounds how long a second builder waits for a peer. A peer mid-build is a SLOW
// holder, not a stuck one, so the second builder QUEUES rather than failing the instant a peer is
// building — the behaviour the previous implementation deliberately chose (two lanes needing the
// same plugin must both succeed). It is a bound, not a retry: past it the waiter gives up and the
// primitive reports the holder, so a genuine wedge is named rather than silently hung.
const buildLockWait = 30 * time.Minute

// acquireBuildLock takes the per-KEY build lock under the RUNTIME root, returning a release that
// is safe to call exactly once.
//
// Keyed by the store key when one exists, so two builders of the SAME input set serialize while
// different input sets never contend. An unkeyed build (a key-computation failure) falls back to
// the plugin name, since two unkeyed builds of one plugin would still race the same view path.
func acquireBuildLock(key, name string) (func() error, error) {
	root, err := storekit.Dir(storekit.RootRuntime)
	if err != nil {
		return nil, fmt.Errorf("resolve the runtime root for the build lock: %w", err)
	}
	lockDir := filepath.Join(root, "buildcache")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("create the build lock dir %s: %w", lockDir, err)
	}
	// The lock file name is a FIXED-LENGTH digest of the key, never the key itself: a store key is
	// the newline-joined composition of every input (which can include a GOWORK path), so using it
	// as a filename overruns the filesystem's name limit — measured, `file name too long`. The hash
	// is the same shape the store uses to name an entry's tag, and it keeps the key's distinction
	// (a different key is a different digest).
	file := SafePluginBinName(filepath.Base(name))
	if key == "" {
		file += "-unkeyed"
	} else {
		sum := sha256.Sum256([]byte(key))
		file += "-" + hex.EncodeToString(sum[:16])
	}
	lockPath := filepath.Join(lockDir, file+".lock")
	// blocking=true and a bounded timeout: queue behind a slow peer, give up at the bound with the
	// holder named.
	return lock.AcquireFileLockWithin(lockPath, true, buildLockWait)
}
