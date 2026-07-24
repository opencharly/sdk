package kit

// build_activity.go — the shared LIVE-build-activity lock directory. Two consumers
// need the SAME path: charly core's acquireBuildActivityLock (charly/filelock.go)
// WRITES a flocked nonce file here for the duration of a `charly box build` engine
// run; candy/plugin-clean's retention engine READS/reaps that set (liveBuildFloor)
// to compute the floor CalVer every FROM pin an in-flight build may still resolve,
// so a completing sibling build can never untag a pin the in-flight build needs.
// Pure primitive (home cache dir + mkdir) — no config dependency — so it lives in
// kit rather than being duplicated across the core/plugin boundary (R3).

import (
	"fmt"
	"os"
	"path/filepath"
)

// BuildActivityDir is the user-scope directory of live build-activity locks — one
// flocked nonce file per in-flight `charly box build` engine run.
func BuildActivityDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("build-activity dir: %w", err)
	}
	dir := filepath.Join(cache, "charly", "locks", "builds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("build-activity dir: %w", err)
	}
	return dir, nil
}
