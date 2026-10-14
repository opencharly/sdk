package storekit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// storekit_test.go — the roots and the store. Every assertion here is about a PROPERTY the
// operator's one-store mandate names: one root, one override, content-addressed bytes, and a
// view that shares the store's bytes rather than copying them.

// TestCacheRootHonoursTheOneOverride pins D-STORE-1's single override: `CHARLY_CACHE_DIR`
// relocates the cache root (and therefore every named store) and is the ONLY environment
// variable involved.
func TestCacheRootHonoursTheOneOverride(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", "/tmp/explicit-charly-cache")
	got, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/explicit-charly-cache" {
		t.Fatalf("CacheDir() = %q, want the CHARLY_CACHE_DIR override", got)
	}
	dir, err := StoreDir("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/tmp/explicit-charly-cache", "artifacts") {
		t.Fatalf("StoreDir = %q, want it under the overridden root", dir)
	}
}

// TestCacheRootDefaultsUnderXDGCache pins the ROOT itself: the cache belongs under
// `$XDG_CACHE_HOME/charly`, NOT under the config root (the pre-existing deviation this package
// ends).
func TestCacheRootDefaultsUnderXDGCache(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	got, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/tmp/xdg-cache", "charly") {
		t.Fatalf("CacheDir() = %q, want $XDG_CACHE_HOME/charly", got)
	}
	cfg, err := Dir(RootConfig)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) == cfg {
		t.Fatal("the cache root must not live under the config root")
	}
}

// TestStoreNameCannotEscapeTheRoot: a store name is one path element, so a store can never be
// created outside the cache root.
func TestStoreNameCannotEscapeTheRoot(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", "/tmp/charly-root")
	for _, bad := range []string{"", ".", "..", "a/b", "../escape"} {
		if _, err := StoreDir(bad); err == nil {
			t.Errorf("StoreDir(%q) accepted a name that is not one path element", bad)
		}
	}
}

// TestStoreIsContentAddressedAndSingleInstance: the same bytes are ONE blob; a second write is a
// no-op; and a view of it is a HARDLINK, so the bytes exist once.
func TestStoreIsContentAddressedAndSingleInstance(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, err := OpenStore("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("the one set of bytes")

	dgst, err := st.PutBlob(payload)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := DigestOf(payload); dgst != want {
		t.Fatalf("PutBlob digest = %q, want the content address %q", dgst, want)
	}
	if !st.HasBlob(dgst) {
		t.Fatal("HasBlob must see a blob that was just written — it is the no-second-fetch primitive")
	}
	// A second write of the same bytes adds nothing.
	if again, err := st.PutBlob(payload); err != nil || again != dgst {
		t.Fatalf("second PutBlob = %q, %v; want the same digest and no error", again, err)
	}
	blobs, err := st.Blobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 1 {
		t.Fatalf("store holds %d blobs after writing the same bytes twice: %v", len(blobs), blobs)
	}

	view := filepath.Join(st.Dir(), "views", "demo", "payload")
	if err := st.LinkBlob(dgst, view); err != nil {
		t.Fatalf("LinkBlob: %v", err)
	}
	blobPath, err := st.BlobPath(dgst)
	if err != nil {
		t.Fatal(err)
	}
	same, err := SameFile(blobPath, view)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Fatal("the view is a COPY of the blob, not a hardlink — the bytes would exist twice")
	}
	got, err := os.ReadFile(view)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("view content = %q, want %q", got, payload)
	}

	// The "cached once" property, measured: naive file sizes exceed UNIQUE-inode sizes by
	// exactly the linked payload.
	naive, unique := treeBytes(t, st.Dir())
	if naive-unique != int64(len(payload)) {
		t.Fatalf("naive(%d) - unique(%d) = %d, want exactly the linked payload (%d)", naive, unique, naive-unique, len(payload))
	}
}

// TestLinkBlobRefusesAMissingBlob: a view of something the store does not hold is an error, not
// an empty file — the store is the source of truth for its own contents.
func TestLinkBlobRefusesAMissingBlob(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, _ := OpenStore("artifacts")
	missing := "sha256:" + "00" + "00000000000000000000000000000000000000000000000000000000000000"
	if err := st.LinkBlob(missing, filepath.Join(st.Dir(), "views", "x")); err == nil {
		t.Fatal("LinkBlob accepted a digest the store does not hold")
	}
}

// TestReadBlobVerifiesTheContentAddress: a blob whose bytes no longer match its digest is a
// hard error — that is what makes a cache hit safe.
func TestReadBlobVerifiesTheContentAddress(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, _ := OpenStore("artifacts")
	dgst, err := st.PutBlob([]byte("honest bytes"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := st.blobPath(dgst)
	if err := os.WriteFile(p, []byte("tampered bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadBlob(dgst); err == nil {
		t.Fatal("ReadBlob accepted bytes that do not match their content address")
	}
}

// TestNamedStoresSeesOnlyLayouts: enumeration is marker-based, so unrelated state under the root
// (a lock directory, a transient tree) is never mistaken for a store.
func TestNamedStoresSeesOnlyLayouts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	if err := os.MkdirAll(filepath.Join(root, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifacts", LayoutFile), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0o755); err != nil {
		t.Fatal(err)
	}
	names, err := NamedStores()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "artifacts" {
		t.Fatalf("NamedStores() = %v, want exactly [artifacts] (locks is not a store)", names)
	}
}

// treeBytes returns (naive total, unique total) over a tree: the naive total counts every
// file's size, the unique total counts each FILE once (by identity, not by name), so their
// difference IS the set of bytes shared through hardlinks. It uses os.SameFile rather than an
// inode field so it is portable across the CI matrix's architectures.
func treeBytes(t *testing.T, dir string) (int64, int64) {
	t.Helper()
	var naive, unique int64
	var seen []os.FileInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		naive += info.Size()
		for _, s := range seen {
			if os.SameFile(s, info) {
				return nil
			}
		}
		seen = append(seen, info)
		unique += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return naive, unique
}

// TestTreeEntryStoresADirectoryOnce: a directory payload (a BuildKit cache export, a repo
// view) is stored as per-file blobs plus one manifest blob naming the tree's OWN media type,
// and materialized as a tree of HARDLINKS — so a directory costs its bytes once, exactly like
// a single file. A tar would have doubled them.
func TestTreeEntryStoresADirectoryOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, err := OpenStore("buildkit-cache")
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "export")
	if err := os.MkdirAll(filepath.Join(src, "blobs", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Two identical files and one different: the identical pair must be ONE blob.
	for _, f := range []struct{ path, body string }{
		{"index.json", `{"schemaVersion":2}`},
		{"blobs/sha256/aaaa", "shared payload"},
		{"blobs/sha256/bbbb", "shared payload"},
	} {
		p := filepath.Join(src, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const mediaType = "application/vnd.opencharly.buildkit-cache.v1"
	entryDigest, err := st.PutTree(src, mediaType)
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	entry, err := st.ReadTree(entryDigest)
	if err != nil {
		t.Fatal(err)
	}
	if entry.MediaType != mediaType {
		t.Fatalf("tree media type = %q, want %q", entry.MediaType, mediaType)
	}
	if len(entry.Files) != 3 {
		t.Fatalf("tree holds %d files, want 3", len(entry.Files))
	}
	// Three files, but only TWO distinct payloads plus the manifest: the duplicate is one blob.
	blobs, err := st.Blobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 3 {
		t.Fatalf("store holds %d blobs, want 3 (two payloads + the manifest): %v", len(blobs), blobs)
	}

	dest := filepath.Join(st.Dir(), "views", "cache-export")
	if err := st.MaterializeTree(entryDigest, dest); err != nil {
		t.Fatalf("MaterializeTree: %v", err)
	}
	for rel, want := range map[string]string{
		"index.json":        `{"schemaVersion":2}`,
		"blobs/sha256/aaaa": "shared payload",
		"blobs/sha256/bbbb": "shared payload",
	} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
	// The reconstructed tree shares the store's bytes.
	naive, unique := treeBytes(t, st.Dir())
	payload := int64(len(`{"schemaVersion":2}`) + 2*len("shared payload"))
	if naive-unique != payload {
		t.Fatalf("naive(%d) - unique(%d) = %d, want exactly the tree's payload bytes (%d)", naive, unique, naive-unique, payload)
	}
}

// TestMaterializeTreeRefusesAnEscapingPath: a stored path is DATA, so a malicious or corrupt
// entry must not be able to write outside the destination.
func TestMaterializeTreeRefusesAnEscapingPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, _ := OpenStore("trees")
	evil := TreeEntry{MediaType: "application/vnd.opencharly.test.v1", Files: map[string]TreeFile{
		"../escaped": {Digest: "sha256:00", Mode: 0o644},
	}}
	raw, err := json.Marshal(evil)
	if err != nil {
		t.Fatal(err)
	}
	dgst, err := st.PutBlob(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MaterializeTree(dgst, filepath.Join(st.Dir(), "views", "x")); err == nil {
		t.Fatal("MaterializeTree accepted a path that escapes the destination")
	}
}

// TestStoreTreeHoldsNoLocks is the guard for the homes rule: a store NEVER places a lock inside
// its own tree (a lock is transient runtime state with no content address, and a digest-keyed
// one could be shared between processes that must not share it). It runs a full store lifecycle
// — put, read, link a single-file view, store and materialize a tree — and then asserts no
// lock-like artifact exists anywhere under the store. It FAILS the moment somebody adds one.
func TestStoreTreeHoldsNoLocks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, err := OpenStore("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("bytes")
	dgst, err := st.PutBlob(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadBlob(dgst); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkBlob(dgst, filepath.Join(st.Dir(), "views", "v", "f")); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := st.PutTree(src, "application/vnd.opencharly.test.v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MaterializeTree(entry, filepath.Join(st.Dir(), "views", "tree")); err != nil {
		t.Fatal(err)
	}

	err = filepath.Walk(st.Dir(), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if strings.Contains(name, ".lock") || (info.IsDir() && name == "locks") {
			t.Errorf("the store tree contains a lock artifact: %s — locks belong to the state/runtime roots, never inside a store", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestEntryIsKeyAddressable is the property a pure content-addressed store lacks: the KEY is
// not derivable from the payload, so it must be recorded — and a lookup by key must be an INDEX
// read, not a payload walk, because the caller has to decide HIT/MISS before it fetches
// anything. Also covers both entry shapes (a single payload and a tree) and key replacement.
func TestEntryIsKeyAddressable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, err := OpenStore("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	// A MISS is answerable without any payload existing.
	if _, ok, err := st.LookupEntry("sha256(url)-first"); err != nil || ok {
		t.Fatalf("LookupEntry on an empty store = ok=%v err=%v, want a clean miss", ok, err)
	}

	// A single-file entry: the payload is the bytes; the KEY is the build inputs.
	payload, err := st.PutBlob([]byte("built binary"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutEntry(Entry{
		Key: "module+gosum+toolchain+platform", MediaType: "application/vnd.opencharly.plugin.binary.v1",
		Payload: payload, Annotations: map[string]string{"resolved": "deadbeef"},
	}); err != nil {
		t.Fatalf("PutEntry: %v", err)
	}

	got, ok, err := st.LookupEntry("module+gosum+toolchain+platform")
	if err != nil || !ok {
		t.Fatalf("LookupEntry = ok=%v err=%v, want a hit", ok, err)
	}
	if got.Payload != payload || got.MediaType == "" {
		t.Fatalf("entry = %+v, want the payload digest and a media type", got)
	}
	if got.Annotations["resolved"] != "deadbeef" {
		t.Fatalf("annotations lost: %+v", got.Annotations)
	}
	// The view of a single-file entry is a hardlink to ITS payload blob.
	view := filepath.Join(st.Dir(), "views", "bin")
	if err := st.LinkBlob(got.Payload, view); err != nil {
		t.Fatal(err)
	}
	blobPath, err := st.BlobPath(payload)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := SameFile(blobPath, view); err != nil || !same {
		t.Fatalf("view is not the payload blob (same=%v err=%v)", same, err)
	}

	// A tree entry: no single payload digest by construction, so the tree manifest is the
	// reference and MaterializeTree is the only view route.
	src := filepath.Join(t.TempDir(), "export")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree, err := st.PutTree(src, "application/vnd.opencharly.buildkit-cache.v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutEntry(Entry{Key: "components-digest", MediaType: "application/vnd.opencharly.buildkit-cache.v1", Tree: tree}); err != nil {
		t.Fatalf("PutEntry(tree): %v", err)
	}
	te, ok, err := st.LookupEntry("components-digest")
	if err != nil || !ok {
		t.Fatalf("tree entry lookup = ok=%v err=%v", ok, err)
	}
	if te.Tree != tree || te.Payload != "" {
		t.Fatalf("tree entry = %+v, want the tree digest and no payload", te)
	}
	if err := st.MaterializeTree(te.Tree, filepath.Join(st.Dir(), "views", "cache")); err != nil {
		t.Fatalf("MaterializeTree: %v", err)
	}

	// Re-keying REPLACES the pointer and keeps the index single-valued.
	if _, err := st.PutEntry(Entry{Key: "components-digest", MediaType: "application/vnd.opencharly.buildkit-cache.v1", Tree: tree, Annotations: map[string]string{"refresh": "1"}}); err != nil {
		t.Fatal(err)
	}
	all, err := st.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("store holds %d entries after a re-key, want 2: %+v", len(all), all)
	}
	refreshed, err := st.ReadEntry("components-digest")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Annotations["refresh"] != "1" {
		t.Fatalf("re-key did not replace the entry: %+v", refreshed)
	}
}

// TestPutEntryRefusesAnUnstoredPayload: an entry pointing at bytes the store does not hold is a
// dangling reference, not an entry.
func TestPutEntryRefusesAnUnstoredPayload(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, _ := OpenStore("artifacts")
	missing := "sha256:" + strings.Repeat("00", 32)
	if _, err := st.PutEntry(Entry{Key: "k", MediaType: "application/vnd.opencharly.test.v1", Payload: missing}); err == nil {
		t.Fatal("PutEntry accepted a payload the store does not hold")
	}
	// Exactly one of payload/tree, and a key and a media type are both required.
	if _, err := st.PutEntry(Entry{MediaType: "application/vnd.opencharly.test.v1"}); err == nil {
		t.Fatal("PutEntry accepted an entry with no key")
	}
	if _, err := st.PutEntry(Entry{Key: "k", Payload: "", Tree: ""}); err == nil {
		t.Fatal("PutEntry accepted an entry with neither payload nor tree")
	}
}

// TestExportedConstantsAndRootsAreContract pins the three exported VALUES that are contracts rather
// than implementation details, so a rename cannot pass unnoticed: the cache-key annotation is shared
// with spec/cache's store (one key vocabulary across every store, which is what lets a reader that
// knows one store read them all), the entry media type is what a consumer dispatches on, and the four
// root names are the D-STORE-1 vocabulary. It also exercises the Root enum through Dir, so the type is
// not merely declared.
func TestExportedConstantsAndRootsAreContract(t *testing.T) {
	if AnnotationCacheKey != "ai.opencharly.cache.key" {
		t.Errorf("AnnotationCacheKey = %q: renaming it breaks every store that reads spec/cache's entries", AnnotationCacheKey)
	}
	if MediaTypeStoreEntry != "application/vnd.opencharly.store.entry.v1+json" {
		t.Errorf("MediaTypeStoreEntry = %q: a consumer dispatches on this value", MediaTypeStoreEntry)
	}
	want := map[Root]string{RootConfig: "config", RootCache: "cache", RootState: "state", RootRuntime: "runtime"}
	for r, name := range want {
		if string(r) != name {
			t.Errorf("Root %q has value %q, want %q", name, string(r), name)
		}
	}
	// Every root resolves, and the runtime root falls back to state rather than to /tmp when
	// XDG_RUNTIME_DIR is unset — a shared world-writable fallback would be the wrong default.
	t.Setenv("XDG_RUNTIME_DIR", "")
	rt, err := Dir(RootRuntime)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Dir(RootState)
	if err != nil {
		t.Fatal(err)
	}
	if rt != st {
		t.Fatalf("Dir(RootRuntime) = %q with no XDG_RUNTIME_DIR, want the state root %q", rt, st)
	}
	for _, r := range []Root{RootConfig, RootCache} {
		if _, err := Dir(r); err != nil {
			t.Errorf("Dir(%s): %v", r, err)
		}
	}
	if _, err := Dir("bogus"); err == nil {
		t.Error("Dir accepted an unknown root name")
	}
}

// TestConcurrentPutBlobOfTheSameBytesSucceeds is the deterministic proof for the temp-file race: two
// goroutines publish the SAME bytes at once. With the old digest-derived temp path (`p + ".tmp"`) they
// shared one file and the loser's rename could fail with ENOENT — a spurious error under concurrency,
// which is a real fault in the store rather than an acceptable race. Deterministic, no load, no sleep:
// both puts must return the same digest with no error, and the store must hold exactly one blob.
func TestConcurrentPutBlobOfTheSameBytesSucceeds(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, err := OpenStore("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("the same bytes, published twice at once")
	const n = 8
	errs := make(chan error, n)
	digests := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dgst, err := st.PutBlob(payload)
			errs <- err
			digests <- dgst
		}()
	}
	wg.Wait()
	close(errs)
	close(digests)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent PutBlob of identical bytes failed: %v", err)
		}
	}
	want, _ := DigestOf(payload)
	for dgst := range digests {
		if dgst != want {
			t.Fatalf("concurrent PutBlob returned %q, want %q", dgst, want)
		}
	}
	blobs, err := st.Blobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 1 {
		t.Fatalf("store holds %d blobs after %d concurrent puts of the same bytes, want 1: %v", len(blobs), n, blobs)
	}
	// No temp files may survive the race.
	ents, err := os.ReadDir(filepath.Join(st.Dir(), "blobs", "sha256"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("a temp file survived the race: %s", e.Name())
		}
	}
}

// TestConcurrentPutEntryOfTheSameKeySucceeds is the deterministic proof for the SECOND call site of
// the atomic write: indexEntry had its own FIXED temp path (`index.json.tmp`), so concurrent PutEntry
// calls for the same key raced exactly as two PutBlob calls did — the loser's rename failed with
// ENOENT. Both writers now go through one helper; this test fails without it.
func TestConcurrentPutEntryOfTheSameKeySucceeds(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	st, err := OpenStore("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := st.PutBlob([]byte("payload for the concurrent entry"))
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.PutEntry(Entry{Key: "the-same-key", MediaType: "application/vnd.opencharly.test.v1", Payload: payload})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent PutEntry of the same key failed: %v", err)
		}
	}
	// The index must still be valid and single-valued, and no temp file may survive.
	entries, err := st.Entries()
	if err != nil {
		t.Fatalf("the store index is unreadable after the race: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("store holds %d entries for one key, want 1", len(entries))
	}
	if _, err := os.Stat(filepath.Join(st.Dir(), "index.json.tmp")); err == nil {
		t.Fatal("a fixed temp file survived the race: index.json.tmp")
	}
}

// TestConcurrentPutEntryOfDistinctKeysKeepsEveryEntry is the proof for the store index's DATA-LOSS bug:
// with the unserialized read-modify-write, N goroutines writing DISTINCT keys all read the same
// snapshot, each appends its descriptor, and the last rename silently drops every other entry. A
// single-key test cannot observe that — the entries are identical, so losing one loses nothing.
func TestConcurrentPutEntryOfDistinctKeysKeepsEveryEntry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	st, err := OpenStore("artifacts")
	if err != nil {
		t.Fatal(err)
	}
	const n = 12
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload, err := st.PutBlob([]byte(fmt.Sprintf("payload %d", i)))
			if err != nil {
				errs <- err
				return
			}
			_, err = st.PutEntry(Entry{
				Key:       fmt.Sprintf("key-%d", i),
				MediaType: "application/vnd.opencharly.test.v1",
				Payload:   payload,
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent PutEntry of a distinct key failed: %v", err)
		}
	}
	entries, err := st.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != n {
		keys := make([]string, 0, len(entries))
		for _, e := range entries {
			keys = append(keys, e.Key)
		}
		sort.Strings(keys)
		t.Fatalf("store holds %d entries after %d concurrent distinct-key puts — entries were LOST: %v", len(entries), n, keys)
	}
	// And every key must be individually resolvable, not merely counted.
	for i := 0; i < n; i++ {
		if _, err := st.ReadEntry(fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatalf("key-%d is missing from the index: %v", i, err)
		}
	}
}
