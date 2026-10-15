package storekit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/spec/lock"
)

// gc.go — the RECLAIM surface: which blobs a store no longer needs, which bytes they hold, and
// the two removals a caller performs once it has decided.
//
// # Why this file exists, and why it is not `OrphanedLayouts`
//
// A store's entries are MUTABLE INDEX SLOTS over IMMUTABLE BYTES. `PutEntry` says it plainly: "a
// later PutEntry with the same key REPLACES the index pointer; the previous payload stays in the
// store as the content-addressed blob it is (immutable, and still reachable by digest)." So every
// re-put of one key leaves its previous payload behind, and nothing in this package could name
// those bytes, let alone free them.
//
// `OrphanedLayouts` does NOT answer this, and its own doc says why: it sizes marker-carrying CHILD
// LAYOUTS inside a store's tree — "a child layout is an orphan BY CONSTRUCTION" — where this file
// sizes UNREFERENCED BLOBS inside `blobs/<alg>/<digest>`. Two different sweeps, and only the
// layout one existed.
//
// # The split, copied rather than invented
//
// `OrphanedLayouts`' doc states the contract this file keeps: "Discovery and sizing only — never
// deletion. Removing is the caller's, so that a sweep's destructive half stays with the surface
// that also reports it (`--dry-run` first, then the removal). This function opens nothing for
// writing." Every function below that walks the store is read-only; the two that remove are
// separate calls the caller makes deliberately, after reporting.
//
// # Reachability, and the one thing a naive sweep gets wrong
//
// A blob is LIVE when an entry reaches it. `Entry` reaches a blob two ways, and the second is
// TRANSITIVE:
//
//	entry.Payload  ->  one blob
//	entry.Tree     ->  the tree's MANIFEST blob  ->  TreeEntry.Files[*].Digest  ->  many blobs
//
// The manifest is itself a blob, so a sweep that subtracted only the `Payload` fields would
// delete every tree's MANIFEST and then every file under it — turning the store's trees into
// unreadable digests. ReachableBlobs walks the third level for exactly that reason.

// UnreferencedBlob is one blob no entry reaches, with the bytes it occupies.
//
// Size is the file's own length, which is what removing it frees: a blob is content-addressed and
// linked — never copied — into views, so its own bytes are the only bytes attributable to it here.
// A caller that reports a total should say "up to", for the same reason `/charly-core:clean` says
// it of `--deep`: a link means the freed space can be less than the sum.
type UnreferencedBlob struct {
	// Digest is the blob's content address, as Blobs() reports it (`<alg>:<hex>`).
	Digest string
	// Size is the blob file's length in bytes.
	Size int64
}

// ReachableBlobs returns every blob digest that the store still needs, directly or through a tree
// manifest.
//
// It is the reference set a sweep subtracts from Blobs(), exposed because a caller may want to
// reason about liveness without listing the unreferenced side. A store with no entries reaches
// nothing and returns an empty set, not an error: an unpopulated store is empty, never broken —
// the same shape NamedStores() and OrphanedLayouts() both use.
//
// # Three references, and the one a first draft misses
//
// The index descriptor for each key carries the ENTRY MANIFEST's digest, and that manifest is
// itself a blob — the thing PutEntry returns. So an entry reaches blobs by three routes:
//
//	index descriptor .Digest  ->  the entry manifest blob
//	entry.Payload             ->  one payload blob
//	entry.Tree                ->  the tree's MANIFEST blob -> TreeEntry.Files[*].Digest -> many
//
// The first is easy to miss, because Entry (the decoded manifest) contains no field for it: the
// pointer lives in `index.json`, one level up. A sweep that reads only Entries() therefore calls
// every entry manifest unreferenced and deletes the store's own descriptors — measured here by a
// test that failed on exactly that, naming a 142-byte blob where an 11-byte payload was expected.
func (s *Store) ReachableBlobs() (map[string]bool, error) {
	idx, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(idx.Manifests))
	for _, d := range idx.Manifests {
		if d.Digest != "" {
			live[d.Digest] = true
		}
	}
	entries, err := s.Entries()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Payload != "" {
			live[e.Payload] = true
		}
		if e.Tree == "" {
			continue
		}
		// The manifest is a blob like any other, and it is reachable BY the entry pointing at it.
		live[e.Tree] = true
		tree, err := s.ReadTree(e.Tree)
		if err != nil {
			// A manifest that cannot be read is a broken store, not an unreadable entry: the
			// caller cannot tell "no blobs" from "could not look", and guessing "no blobs" would
			// make every file under this tree collectable. Fail loudly.
			return nil, fmt.Errorf("reachability: read tree %s (entry %q): %w", e.Tree, e.Key, err)
		}
		for _, f := range tree.Files {
			if f.Digest != "" {
				live[f.Digest] = true
			}
		}
	}
	return live, nil
}

// UnreferencedBlobs lists the store's blobs that no entry reaches, with sizes, sorted by digest.
//
// READ-ONLY. It removes nothing — see the header. The caller decides what to do with the list,
// and RemoveBlob is how it does it.
func (s *Store) UnreferencedBlobs() ([]UnreferencedBlob, error) {
	live, err := s.ReachableBlobs()
	if err != nil {
		return nil, err
	}
	blobs, err := s.Blobs()
	if err != nil {
		return nil, err
	}
	var out []UnreferencedBlob
	for _, dgst := range blobs {
		if live[dgst] {
			continue
		}
		p, err := s.blobPath(dgst)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				// Listed then gone: a concurrent removal won the race. Not an error, and not a
				// candidate either — reporting it would hand the caller a digest to remove that
				// no longer exists, and its removal would then be the very race this skip avoids.
				continue
			}
			return nil, fmt.Errorf("size blob %s: %w", dgst, err)
		}
		out = append(out, UnreferencedBlob{Digest: dgst, Size: fi.Size()})
	}
	return out, nil
}

// RemoveBlob deletes ONE blob by digest. It is the caller's destructive half, never implicit.
//
// It REFUSES a digest an entry still reaches: a sweep that has not re-read the store since it
// listed candidates must not be able to free live bytes on a second call. That check is one
// Entries() walk, and it is the difference between a GC and a delete primitive.
func (s *Store) RemoveBlob(dgst string) error {
	live, err := s.ReachableBlobs()
	if err != nil {
		return err
	}
	if live[dgst] {
		return fmt.Errorf("refusing to remove %s: an entry still reaches it", dgst)
	}
	p, err := s.blobPath(dgst)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove blob %s: %w", dgst, err)
	}
	return nil
}

// DeleteEntry drops an entry's index pointer, returning whether an entry was there.
//
// It removes the POINTER, never the payload: the payload is content-addressed and may be shared
// with another entry, so freeing it is a decision only a reachability sweep can make. That is why
// this and UnreferencedBlobs are one change rather than two — a store whose index cannot drop an
// entry keeps every digest it ever wrote live forever, and a sweep over such a store would be
// correct about a set that can never shrink.
//
// The read-modify-write is serialized through the SAME lock indexEntry uses, so a delete racing a
// put cannot lose the other's write. It deliberately does NOT sweep: the caller runs
// UnreferencedBlobs afterwards if it wants the bytes back.
func (s *Store) DeleteEntry(key string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("a store entry is identified by its key")
	}
	lockPath, err := s.indexLockPath()
	if err != nil {
		return false, err
	}
	release, err := lock.AcquireFileLockWithin(lockPath, true, indexLockWait)
	if err != nil {
		return false, fmt.Errorf("lock the store index: %w", err)
	}
	defer func() { _ = release() }()

	idx, err := s.readIndex()
	if err != nil {
		return false, err
	}
	tag := entryTag(key)
	kept := idx.Manifests[:0]
	removed := false
	for _, d := range idx.Manifests {
		if !removed && (d.Annotations[AnnotationCacheKey] == key || d.Annotations[AnnotationRefName] == tag) {
			removed = true
			continue
		}
		kept = append(kept, d)
	}
	if !removed {
		return false, nil
	}
	idx.Manifests = kept
	raw, err := json.Marshal(idx)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(filepath.Join(s.dir, "index.json"), raw); err != nil {
		return false, err
	}
	return true, nil
}

// BlobBytes sums the sizes of a candidate list, for a report line.
func BlobBytes(blobs []UnreferencedBlob) int64 {
	var n int64
	for _, b := range blobs {
		n += b.Size
	}
	return n
}
