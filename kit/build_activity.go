package kit

// build_activity.go — the shared LIVE-build-activity lock directory. Two consumers
// need the SAME path: candy/plugin-box's acquireBuildActivityLock WRITES a flocked
// nonce file here for the duration of a `charly box build` engine run (the file's
// content is the build's CalVer, the floor of every FROM pin its Containerfiles carry);
// candy/plugin-clean's retention engine READS/reaps that set (liveBuildFloor) to
// compute the floor CalVer every FROM pin an in-flight build may still resolve, so a
// completing sibling build can never untag a pin the in-flight build needs. Pure
// primitive (the one cache root + mkdir) — no config dependency — so it lives in kit
// rather than being duplicated across the two plugin candies (R3).
//
// The root comes from storekit.Dir(RootCache), NEVER a hand-built path: that resolver
// is the ONE place a charly path is composed, and it is the only place `CHARLY_CACHE_DIR`
// is read (D-STORE-1, TODO 7.2). A hand-built `$XDG_CACHE_HOME/charly` here would put
// the build locks of a `CHARLY_CACHE_DIR`-relocated install under a DIFFERENT root from
// the builds they gate, so a sibling build's floor CalVer could not see them.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/sdk/storekit"
)

// BuildActivityDir is the user-scope directory of live build-activity locks — one
// flocked nonce file per in-flight `charly box build` engine run.
func BuildActivityDir() (string, error) {
	cache, err := storekit.Dir(storekit.RootCache)
	if err != nil {
		return "", fmt.Errorf("build-activity dir: %w", err)
	}
	dir := filepath.Join(cache, "locks", "builds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("build-activity dir: %w", err)
	}
	return dir, nil
}
