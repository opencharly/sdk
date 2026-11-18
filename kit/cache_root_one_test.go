package kit

// cache_root_one_test.go — D-STORE-1's "one resolver" clause, on the one place this package built a
// cache path by hand.
//
// DESIGN.md:2482 (D-STORE-1): cache is `$XDG_CACHE_HOME/charly` with the SINGLE override
// `CHARLY_CACHE_DIR`, and the resolver is the contract surface's (`spec/storeroot`, re-exported by
// `sdk/storekit`) — "no caller builds a `.cache` path by hand". This test drives the real fetch
// entry point and asserts WHICH directory it created: the resolver's, never `$HOME/.cache/charly`.
//
// The URL is a closed port, so the fetch fails fast and the assertion is about the cache ROOT, not
// about a download. The directory is created before the download, so a failed fetch still proves the
// decision.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFetchArtifactCacheRootFollowsTheOneCacheRoot(t *testing.T) {
	home := t.TempDir()
	cacheRoot := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CHARLY_CACHE_DIR", cacheRoot)

	// Port 1 on loopback refuses immediately; the error is expected and irrelevant.
	_, _ = FetchArtifact(VmSource{URL: "http://127.0.0.1:1/p72-one-cache-root.qcow2"}, ".qcow2")

	if _, err := os.Stat(filepath.Join(cacheRoot, "vm-images")); err != nil {
		t.Fatalf("the artifact cache was not created under the ONE cache root (%s/vm-images): %v",
			cacheRoot, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "charly", "vm-images")); err == nil {
		t.Fatalf("a hand-built %s/.cache/charly/vm-images was created — D-STORE-1 has ONE resolver "+
			"and CHARLY_CACHE_DIR is its only override", home)
	}
}
