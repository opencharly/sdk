package kit

// filelock.go — the ONE advisory-flock primitive, shared by charly core (filelock.go's
// specialized wrappers) AND the compiled-in candy/plugin-preempt (the resource-arbiter's
// per-acquire ledger lock). It lives in kit so there is a SINGLE implementation across the
// module boundary (R3) — kit imports only the stdlib, so both sides link the same code.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrLockBusy is returned by a NON-blocking AcquireFileLock when another holder already owns the
// lock. Callers detect it with errors.Is to render a precise "already in progress" message.
var ErrLockBusy = errors.New("file lock held by another process")

// AcquireFileLock takes an advisory flock on path (creating the file + parent dirs on demand) and
// returns a release closure that unlocks + closes.
//
// blocking selects the contention behavior:
//   - true  → LOCK_EX: wait until the lock is free (serialize, never fail).
//   - false → LOCK_EX|LOCK_NB: return ErrLockBusy immediately when another holder exists.
//
// The lock file is deliberately NOT unlinked on release (unlinking a held lock races a waiter
// that already opened the prior inode). flock is per-open-file-description, so two acquires of the
// same path — even within ONE process — contend, which the duplicate-run guard relies on.
func AcquireFileLock(path string, blocking bool) (release func() error, err error) {
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return nil, fmt.Errorf("create lock dir %s: %w", filepath.Dir(path), mkErr)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	how := syscall.LOCK_EX
	if !blocking {
		how |= syscall.LOCK_NB
	}
	if flockErr := syscall.Flock(int(f.Fd()), how); flockErr != nil {
		_ = f.Close()
		if !blocking {
			return nil, fmt.Errorf("%s: %w", path, ErrLockBusy)
		}
		return nil, fmt.Errorf("flock %s: %w", path, flockErr)
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "pid=%d\n", os.Getpid())
	return func() error {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return f.Close()
	}, nil
}

// ImageBuildLockPath is the pure per-image build-lock key derivation: the
// user-cache lock file for an image ref, with the :tag stripped (preserving any
// registry:port colon) so every CalVer build of one image shares ONE lock — a
// shared intermediate built COLD once while distinct leaves fan out in parallel.
// Shared across the module boundary (R3) so charly core's acquireImageBuildLock
// AND the compiled-in candy/plugin-build DRIVE derive the byte-identical path.
func ImageBuildLockPath(fullTag string) (string, error) {
	ref := fullTag
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i] // strip :<tag>, preserving any registry:port colon
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("image build lock: %w", err)
	}
	dir := filepath.Join(cache, "charly", "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("image build lock dir: %w", err)
	}
	sum := sha256.Sum256([]byte(ref))
	return filepath.Join(dir, "image-"+hex.EncodeToString(sum[:8])+".lock"), nil
}

// AcquireImageBuildLock takes the blocking per-image build lock for fullTag.
func AcquireImageBuildLock(fullTag string) (func() error, error) {
	path, err := ImageBuildLockPath(fullTag)
	if err != nil {
		return nil, err
	}
	return AcquireFileLock(path, true)
}

// AcquireLocalPkgBuildLock serializes concurrent localpkg builds sharing a source dir.
func AcquireLocalPkgBuildLock(srcDir string) (func() error, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("localpkg build lock: %w", err)
	}
	dir := filepath.Join(cache, "charly", "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("localpkg build lock dir: %w", err)
	}
	sum := sha256.Sum256([]byte(srcDir))
	return AcquireFileLock(filepath.Join(dir, "localpkg-"+hex.EncodeToString(sum[:8])+".lock"), true)
}
