package storekit

import (
	"os"
	"path/filepath"
	"testing"
)

// gc_test.go — the mutation answer for the reclaim surface.
//
// Every test here is written so that it FAILS if the production change is reverted: before
// gc.go, there was no way to DELETE an entry, no way to NAME an unreferenced blob, and no way to
// remove one. The tests therefore do not merely exercise new functions — they assert the two
// properties that made the surface necessary:
//
//	1. a re-put entry's previous payload becomes UNREACHABLE and is then named by the sweep;
//	2. a tree's MANIFEST and every file under it stay REACHABLE, which is what a sweep that
//	   subtracted only Entry.Payload would get wrong.

// newTestStore makes an empty store under a temp cache root and returns it.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	s, err := OpenStore("test")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	return s
}

// putPayload stores data as a blob and indexes it under key.
func putPayload(t *testing.T, s *Store, key, data string) string {
	t.Helper()
	dgst, err := s.PutBlob([]byte(data))
	if err != nil {
		t.Fatalf("PutBlob(%q): %v", data, err)
	}
	if _, err := s.PutEntry(Entry{Key: key, MediaType: "application/vnd.test+json", Payload: dgst}); err != nil {
		t.Fatalf("PutEntry(%q): %v", key, err)
	}
	return dgst
}

// TestUnreferencedBlobsNamesOnlyWhatNothingReaches is the change's core claim. A store with one
// entry has exactly one reachable blob; the sweep names nothing. After the same key is re-put
// with different bytes — the documented "previous payload stays in the store" case — the OLD
// blob becomes unreachable and the sweep names exactly it, and nothing else.
func TestUnreferencedBlobsNamesOnlyWhatNothingReaches(t *testing.T) {
	s := newTestStore(t)

	first := putPayload(t, s, "the-key", "first bytes")

	cand, err := s.UnreferencedBlobs()
	if err != nil {
		t.Fatalf("UnreferencedBlobs (after one put): %v", err)
	}
	if len(cand) != 0 {
		t.Fatalf("a store whose only blob is referenced named %d candidate(s): %+v", len(cand), cand)
	}

	// The re-put. PutEntry's own contract says the previous payload stays as a blob; that is the
	// leak this surface exists for.
	second := putPayload(t, s, "the-key", "second bytes")
	if first == second {
		t.Fatal("the two payloads hashed equal; the test cannot distinguish them")
	}

	cand, err = s.UnreferencedBlobs()
	if err != nil {
		t.Fatalf("UnreferencedBlobs (after re-put): %v", err)
	}
	// A re-put orphans TWO blobs, and the count is the LESS interesting half of it: the
	// superseded PAYLOAD (the documented leak) and the superseded ENTRY MANIFEST, because the
	// index keeps one descriptor per key and the older manifest is a blob like any other. Assert
	// each by identity, so the test states the property rather than an incidental total.
	var payloadCand *UnreferencedBlob
	for i := range cand {
		if cand[i].Digest == first {
			payloadCand = &cand[i]
		}
		if cand[i].Digest == second {
			t.Fatalf("the CURRENT payload %s was named as unreferenced", second)
		}
	}
	if payloadCand == nil {
		t.Fatalf("the superseded payload %s is not among the candidates: %+v", first, cand)
	}
	if payloadCand.Size != int64(len("first bytes")) {
		t.Fatalf("superseded payload size = %d, want %d", payloadCand.Size, len("first bytes"))
	}
	if got := BlobBytes(cand); got < int64(len("first bytes")) {
		t.Fatalf("BlobBytes = %d, want at least %d", got, len("first bytes"))
	}
}

// TestUnreferencedBlobsKeepsEveryTreeBlob is the test a naive sweep fails. A tree entry reaches
// its MANIFEST and every file under it; a sweep that subtracted only Entry.Payload would call all
// of them unreferenced and delete a readable tree into an unreadable digest.
func TestUnreferencedBlobsKeepsEveryTreeBlob(t *testing.T) {
	s := newTestStore(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := s.PutTree(dir, "application/vnd.test.tree+json")
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	if _, err := s.PutEntry(Entry{Key: "the-tree", MediaType: "application/vnd.test+json", Tree: manifest}); err != nil {
		t.Fatalf("PutEntry(tree): %v", err)
	}

	cand, err := s.UnreferencedBlobs()
	if err != nil {
		t.Fatalf("UnreferencedBlobs: %v", err)
	}
	if len(cand) != 0 {
		t.Fatalf("a tree entry's manifest and files must all stay reachable; named %+v", cand)
	}

	live, err := s.ReachableBlobs()
	if err != nil {
		t.Fatalf("ReachableBlobs: %v", err)
	}
	if !live[manifest] {
		t.Fatalf("the MANIFEST %s is not reachable; a sweep would delete the tree's own identity", manifest)
	}
	tree, err := s.ReadTree(manifest)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	for path, f := range tree.Files {
		if !live[f.Digest] {
			t.Fatalf("tree file %q (%s) is not reachable; a sweep would delete its contents", path, f.Digest)
		}
	}
}

// TestRemoveBlobRefusesWhatAnEntryStillReaches pins the guard that separates this from a delete
// primitive: a caller that listed candidates and then re-read them must not be able to free live
// bytes on a second call.
func TestRemoveBlobRefusesWhatAnEntryStillReaches(t *testing.T) {
	s := newTestStore(t)
	live := putPayload(t, s, "still-here", "live bytes")

	if err := s.RemoveBlob(live); err == nil {
		t.Fatalf("RemoveBlob(%s) removed a blob an entry still reaches", live)
	}
	if _, err := os.Stat(mustBlobPath(t, s, live)); err != nil {
		t.Fatalf("the refused blob is gone from disk: %v", err)
	}
}

// TestRemoveBlobFreesASupersededPayloadAndOnlyThat walks the whole documented sequence: a re-put
// orphans the old payload, the sweep names it, and removing it drops that blob while leaving the
// current one intact — the bytes a caller actually gets back.
func TestRemoveBlobFreesASupersededPayloadAndOnlyThat(t *testing.T) {
	s := newTestStore(t)
	first := putPayload(t, s, "k", "old")
	_ = putPayload(t, s, "k", "new")

	cand, err := s.UnreferencedBlobs()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cand {
		if c.Digest == first {
			found = true
		}
	}
	if !found {
		t.Fatalf("the superseded payload %s is not among the candidates: %+v", first, cand)
	}
	if err := s.RemoveBlob(first); err != nil {
		t.Fatalf("RemoveBlob(superseded): %v", err)
	}
	if _, err := os.Stat(mustBlobPath(t, s, first)); !os.IsNotExist(err) {
		t.Fatalf("the superseded blob still exists after removal: %v", err)
	}
	// And the sweep no longer names it: a removed blob does not come back as a candidate.
	cand, err = s.UnreferencedBlobs()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cand {
		if c.Digest == first {
			t.Fatalf("the removed blob %s is still a candidate", first)
		}
	}
}

// TestDeleteEntryDropsThePointerAndNothingElse is the mutation test for the entry half: deleting
// an entry makes its payload the sweep's business, and no other entry's blobs are affected.
func TestDeleteEntryDropsThePointerAndNothingElse(t *testing.T) {
	newTestStore(t)
	root := os.Getenv("CHARLY_CACHE_DIR")
	s, err := OpenStore("test")
	if err != nil || s.Dir() == "" || root == "" {
		t.Fatalf("store reopen: %v", err)
	}

	gone := putPayload(t, s, "doomed", "payload-of-doomed")
	kept := putPayload(t, s, "keeper", "payload-of-keeper")

	removed, err := s.DeleteEntry("doomed")
	if err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if !removed {
		t.Fatal("DeleteEntry reported nothing removed for an entry that exists")
	}

	// The pointer is gone: a second delete is a no-op, not an error.
	again, err := s.DeleteEntry("doomed")
	if err != nil {
		t.Fatalf("DeleteEntry (second): %v", err)
	}
	if again {
		t.Fatal("DeleteEntry removed the same entry twice")
	}

	entries, err := s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Key == "doomed" {
			t.Fatal("the deleted entry is still in the index")
		}
	}

	cand, err := s.UnreferencedBlobs()
	if err != nil {
		t.Fatal(err)
	}
	foundGone := false
	for _, c := range cand {
		if c.Digest == gone {
			foundGone = true
		}
	}
	if !foundGone {
		t.Fatalf("the deleted entry's payload %s is not a candidate: %+v", gone, cand)
	}
	// The keeper is untouched: the delete dropped a pointer, not a store.
	live, err := s.ReachableBlobs()
	if err != nil {
		t.Fatal(err)
	}
	if !live[kept] {
		t.Fatalf("the surviving entry's payload %s stopped being reachable", kept)
	}
}

// TestDeleteEntryRefusesAnEmptyKey pins the one input that cannot identify an entry.
func TestDeleteEntryRefusesAnEmptyKey(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DeleteEntry(""); err == nil {
		t.Fatal("DeleteEntry(\"\") returned no error")
	}
}

func mustBlobPath(t *testing.T, s *Store, dgst string) string {
	t.Helper()
	p, err := s.blobPath(dgst)
	if err != nil {
		t.Fatalf("blobPath(%s): %v", dgst, err)
	}
	return p
}
