package storekit

import (
	"os"
	"path/filepath"
	"testing"
)

// orphans_test.go — the orphan-discovery mechanism. Each arm pins a property the predicate exists
// for, so a change that widened it (or narrowed it) fails here rather than in a consumer's sweep.

// writeLayout makes a directory that LOOKS like an OCI image layout — a marker plus one payload —
// and returns the bytes a sweep would reclaim from it (every file, marker included).
func writeLayout(t *testing.T, dir string, payload []byte) int64 {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	marker := []byte(`{"imageLayoutVersion":"1.0.0"}`)
	if err := os.WriteFile(filepath.Join(dir, LayoutFile), marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payload"), payload, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	return int64(len(marker) + len(payload))
}

// TestOrphanedLayoutsFindsMarkerChildrenAndSizesThem pins the predicate and the size together: a
// marker-carrying child is returned BY CONSTRUCTION (that is the shape a retired per-ref writer
// leaves under the store's own path), a child without the marker is not, a plain file at the root is
// never a candidate, and the reported bytes count every file in the tree the sweep would remove.
func TestOrphanedLayoutsFindsMarkerChildrenAndSizesThem(t *testing.T) {
	root := t.TempDir()
	// The store's own shape, which is NOT an orphan: blobs/, index.json, and the root's own marker.
	if err := os.MkdirAll(filepath.Join(root, "blobs", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.json"), []byte(`{"schemaVersion":2,"manifests":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, LayoutFile), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wantBytes := writeLayout(t, filepath.Join(root, "quay.io_old_shape_v1"), make([]byte, 4096))
	// A child that is NOT a layout must be left alone — a sweep that took it would remove live state.
	if err := os.MkdirAll(filepath.Join(root, "not-a-layout"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-layout", "keep"), []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	orphans, err := OrphanedLayouts(root)
	if err != nil {
		t.Fatalf("OrphanedLayouts: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("OrphanedLayouts = %+v, want exactly the one marker-carrying child", orphans)
	}
	if want := filepath.Join(root, "quay.io_old_shape_v1"); orphans[0].Dir != want {
		t.Fatalf("orphan dir = %q, want %q", orphans[0].Dir, want)
	}
	if orphans[0].Bytes != wantBytes {
		t.Fatalf("orphan bytes = %d, want %d (marker %d + payload %d)", orphans[0].Bytes, wantBytes, len(`{"imageLayoutVersion":"1.0.0"}`), 4096)
	}
	// Discovery never deletes: the orphan is still on disk after the call.
	if _, err := os.Stat(orphans[0].Dir); err != nil {
		t.Fatalf("OrphanedLayouts removed something (%v) — discovery and sizing only", err)
	}
}

// TestOrphanedLayoutsIsOneLevelOnly pins the depth of the predicate: only DIRECT children are
// candidates. A layout nested deeper is not this function's business, and a caller that wants a
// recursive sweep must say so rather than inherit it by accident.
func TestOrphanedLayoutsIsOneLevelOnly(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "group")
	if err := os.MkdirAll(outer, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLayout(t, filepath.Join(outer, "nested"), make([]byte, 16))

	orphans, err := OrphanedLayouts(root)
	if err != nil {
		t.Fatalf("OrphanedLayouts: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("OrphanedLayouts = %+v, want none: `group` carries no marker, and its child is one level too deep", orphans)
	}
}

// TestOrphanedLayoutsOnAnAbsentOrEmptyRootIsNotAnError pins the missing-root shape the rest of this
// package uses (NamedStores() returns an empty list for a missing cache root, never an error): a
// sweep of a root that was never populated is a no-op, not a failure.
func TestOrphanedLayoutsOnAnAbsentOrEmptyRootIsNotAnError(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "never-created")
	orphans, err := OrphanedLayouts(absent)
	if err != nil {
		t.Fatalf("OrphanedLayouts(absent) = %v, want no error", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("OrphanedLayouts(absent) = %+v, want none", orphans)
	}
	empty := t.TempDir()
	if orphans, err = OrphanedLayouts(empty); err != nil || len(orphans) != 0 {
		t.Fatalf("OrphanedLayouts(empty) = (%+v, %v), want (none, nil)", orphans, err)
	}
}

// TestOrphanedLayoutsOrderIsStable pins the ordering a caller's report depends on: ReadDir sorts by
// name, so two calls on the same tree return the same order. An unstable order would make a
// consumer's pasted evidence differ between runs for no reason.
func TestOrphanedLayoutsOrderIsStable(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta_v1", "alpha_v1", "mid_v1"} {
		writeLayout(t, filepath.Join(root, name), []byte(name))
	}
	first, err := OrphanedLayouts(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OrphanedLayouts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("OrphanedLayouts = %+v, want all three markers", first)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order is unstable: %v then %v", first, second)
		}
	}
	want := []string{"alpha_v1", "mid_v1", "zeta_v1"}
	for i, o := range first {
		if filepath.Base(o.Dir) != want[i] {
			t.Fatalf("order = %v, want sorted by name %v", first, want)
		}
	}
}

// TestOrphanedLayoutsAgreesWithNamedStoresOnTheMarker is the cross-check that keeps the two readings
// of `LayoutFile` from drifting: every directory this function calls an orphan is a directory
// NamedStores() would call a STORE if it sat at a cache root — same marker, two directions. If a
// future change taught one side a second marker, this arm fails.
func TestOrphanedLayoutsAgreesWithNamedStoresOnTheMarker(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", cache)
	writeLayout(t, filepath.Join(cache, "registry"), []byte("a store"))

	named, err := NamedStores()
	if err != nil {
		t.Fatalf("NamedStores: %v", err)
	}
	if len(named) != 1 || named[0] != "registry" {
		t.Fatalf("NamedStores = %v, want [registry]", named)
	}
	// Inside the store's own tree, the same shape is an orphan — that is the inversion this function
	// exists for, exercised on the very directory NamedStores() just called a store.
	orphanBytes := writeLayout(t, filepath.Join(cache, "registry", "old_ref_v1"), []byte("orphan"))
	orphans, err := OrphanedLayouts(filepath.Join(cache, "registry"))
	if err != nil {
		t.Fatalf("OrphanedLayouts: %v", err)
	}
	if len(orphans) != 1 || orphans[0].Bytes != orphanBytes {
		t.Fatalf("OrphanedLayouts = %+v, want the child layout (%d bytes)", orphans, orphanBytes)
	}
}
