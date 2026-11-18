package loaderkit

// cache_root_one_test.go — D-STORE-1's "one resolver" clause, on the ONE place sdk still carried a
// second override.
//
// DESIGN.md:2482 (D-STORE-1): the four roots are resolved by ONE resolver on the contract surface
// (`spec/storeroot`, re-exported by `sdk/storekit`), cache is `$XDG_CACHE_HOME/charly` with the
// SINGLE override `CHARLY_CACHE_DIR`. A per-cache override variable is a second resolver, so
// `CHARLY_MATERIALIZED_CACHE` has to be gone — not merely deprecated (R5: no dual path).
//
// This test fails while the override is honoured, and the assertion names the resolver it expects.

import (
	"path/filepath"
	"testing"
)

func TestMaterializedCacheDirIgnoresTheRetiredOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)

	// The retired second override, set to something the ONE resolver can never produce.
	retired := t.TempDir()
	t.Setenv("CHARLY_MATERIALIZED_CACHE", retired)

	got, err := materializedCacheDir()
	if err != nil {
		t.Fatalf("materializedCacheDir: %v", err)
	}
	want := filepath.Join(root, "materialized")
	if got != want {
		t.Fatalf("materialized cache root = %q, want %q — CHARLY_MATERIALIZED_CACHE is a second "+
			"resolver, and D-STORE-1 admits exactly one (CHARLY_CACHE_DIR)", got, want)
	}
}
