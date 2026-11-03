package storekit

import (
	"fmt"
	"os"
	"path/filepath"
)

// Orphan is one discovered layout tree: its directory and the bytes a sweep would reclaim.
//
// Bytes counts EVERY file in the tree — including the `oci-layout` marker that makes it a layout in
// the first place — because that is what `os.RemoveAll` actually reclaims. A size that omitted the
// marker under-reported a real sweep by the marker's own bytes (measured while writing the first
// consumer's assertion).
type Orphan struct {
	Dir   string
	Bytes int64
}

// OrphanedLayouts returns the marker-carrying child layout trees of root, each sized, in ReadDir
// order (sorted by name, so a caller's output is stable).
//
// # The predicate is NamedStores() INVERTED, and that is the whole point
//
// NamedStores() calls a marker-carrying child of the cache root a STORE. The same directory, found
// inside a store's own tree, is junk: a store keeps every byte in `blobs/<alg>/<digest>` and its
// entries in `index.json`, and never reads a child layout. One marker constant (`LayoutFile`), read
// in two directions, so the two readings cannot drift apart.
//
// # The CALLER names the root, because liveness is policy and this is mechanism
//
// This function does NOT decide which layouts are orphaned, and it cannot: `NamedStores()` answers
// nothing here either — it returns exactly the marker-carrying children in question. A function that
// tried to guess "no merged writer still writes this" would be policy wearing a mechanism's name
// (the boundary law), so the contract is the opposite: the caller passes a root in which a
// marker-carrying child cannot be live. Two such roots exist, and both are the caller's to justify:
//
//   - A STORE'S OWN TREE (`Store.Dir()`), where a child layout is an orphan BY CONSTRUCTION — the
//     shape a retired per-ref writer leaves behind when the store moves into the path it used to
//     write under.
//   - A CACHE ROOT the migration table has RETIRED (a root no merged writer still writes), where the
//     caller's justification is that retirement, recorded in that migration.
//
// # Discovery and sizing only — never deletion
//
// Removing is the caller's, so that a sweep's destructive half stays with the surface that also
// reports it (`--dry-run` first, then the removal). This function opens nothing for writing.
//
// A root that does not exist yet has no orphans and is never an error — the same shape
// `NamedStores()` uses for a missing cache root.
func OrphanedLayouts(root string) ([]Orphan, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var out []Orphan
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, LayoutFile)); err != nil {
			continue
		}
		out = append(out, Orphan{Dir: dir, Bytes: dirBytes(dir)})
	}
	return out, nil
}

// dirBytes sums every file under dir. A walk error (a vanished file, a permission) is skipped rather
// than fatal: this is a SIZE for a report, and a sweep's own removal is where a real failure must
// surface.
func dirBytes(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}
