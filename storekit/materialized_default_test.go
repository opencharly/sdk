package storekit

// materialized_default_test.go — the OWNER of D-STORE-1's default-path claim.
//
// The materialized-tree cache moved its ROOT from `spec/cache.StoreDir("materialized")` to
// `storekit.StoreDir("materialized")`. The two must resolve to the SAME directory for a host that
// never set an override, or an upgrade silently relocates a cache — which would be a behaviour
// change the row (TODO 7.2) forbids.
//
// No compiler can answer this and no comment proves it: the instrument is a resolution with the
// environment unset. This test is that instrument, and it asserts BOTH halves — the documented
// default, and that the two resolvers agree — so a future change to either one fails here rather
// than in a user's cache directory.

import (
	"path/filepath"
	"testing"

	specache "github.com/opencharly/spec/cache"
)

func TestTheMaterializedCacheDefaultIsUnchangedByTheMove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Unset every override: XDG must fall back, and neither cache variable may exist.
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("CHARLY_CACHE_DIR", "")
	t.Setenv("CHARLY_MATERIALIZED_CACHE", "")

	want := filepath.Join(home, ".cache", "charly", "materialized")

	before, err := specache.StoreDir("materialized")
	if err != nil {
		t.Fatalf("spec/cache.StoreDir: %v", err)
	}
	after, err := StoreDir("materialized")
	if err != nil {
		t.Fatalf("storekit.StoreDir: %v", err)
	}
	if before != want {
		t.Fatalf("the resolver being replaced returns %q, want %q — the fixture is not the world the claim describes", before, want)
	}
	if after != want {
		t.Fatalf("the new resolver returns %q, want %q: a host that never set an override would be MOVED by the upgrade", after, want)
	}
	// And the claim's own sentence: the two agree, so "nothing changes" is a measurement.
	if before != after {
		t.Fatalf("the two resolvers disagree (%q vs %q): the row's no-behaviour-change claim is false", before, after)
	}
}
