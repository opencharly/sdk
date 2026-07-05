package kit

import (
	"reflect"
	"testing"
)

// TestExactRepoRefs proves the overlay-drop selection removes ONLY the deploy's own `<name>-overlay`
// repo, never the base image or a cross-deploy image that podman's reference filter leaks via a shared
// image ID. Without the basename-exact check this test FAILS (the naive "rmi every emitted ref" would
// return the base + cross-deploy refs) — the destructive collateral the RCA found in the old core code.
func TestExactRepoRefs(t *testing.T) {
	// The exact over-match podman emitted on the real host for `reference=check-addcandy-pod-overlay`:
	// the overlay AND the base AND cross-bed bases sharing the same image ID.
	out := "" +
		"ghcr.io/opencharly/check-addcandy-pod 2026.186.0804\n" +
		"localhost/check-addcandy-pod-overlay 0446fad168fa\n" +
		"localhost/check-addcandy-pod-overlay 6480218199c0\n" +
		"ghcr.io/opencharly/check-enc-pod 2026.185.1200\n" +
		"localhost/check-tunnel-pod-overlay 111122223333\n" // a DIFFERENT deploy's overlay — must NOT match

	got := exactRepoRefs(out, "check-addcandy-pod-overlay")
	want := []string{
		"localhost/check-addcandy-pod-overlay:0446fad168fa",
		"localhost/check-addcandy-pod-overlay:6480218199c0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("exactRepoRefs selected the wrong refs\n got:  %v\n want: %v", got, want)
	}

	// Empty / whitespace-only output → nothing to remove (no panic, no bogus ":" ref).
	if refs := exactRepoRefs("  \n ", "x-overlay"); len(refs) != 0 {
		t.Errorf("empty output must select nothing, got %v", refs)
	}
}
