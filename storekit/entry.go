package storekit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// entry.go — KEY-ADDRESSABLE entries: the one thing a pure content-addressed store cannot do
// by itself.
//
// A content-addressed store can always answer "is this DIGEST present". It cannot answer "is
// this KEY present", because in every cache that matters the key is NOT derivable from the
// payload:
//
//   - a built provider binary keys on its build INPUTS (module source, go.sum, toolchain,
//     platform) — two keys can share one byte-string, and one key can never be two;
//   - a schema-coupled entry keys on a components digest (config + schema + loader identity);
//   - a downloaded image keys on the SOURCE LOCATION (`sha256(url)`), deliberately, because
//     that must be computable BEFORE the artifact exists — it is what lets an unpinned rolling
//     image be reused at all.
//
// So the key is information the store does not have and must be handed. The ENTRY is where a
// key and a payload reference meet: ONE manifest blob carrying the key, the payload's media
// type, and a reference to either a single payload blob or a tree manifest.
//
// Lookup is by KEY through the store's own `index.json` — an OCI-layout index, where each
// entry is a descriptor whose tag is derived from the key and whose annotation carries the RAW
// key (`ai.opencharly.cache.key`, the same key vocabulary `spec/cache` already uses: one key
// name across every store). That is what makes a lookup ONE small index read rather than a
// directory walk, which is exactly the property a caller needs when it must decide HIT/MISS
// before it fetches anything.
//
// THE DIGEST ADDRESSES THE BYTES; THE KEY ADDRESSES THE ENTRY. Two entries may legitimately
// share blobs — the same disk image fetched from two URLs — while carrying different keys:
// sharing at the blob level IS the "cached once" mandate, and per-key identity is what a
// consumer needs on top of it. Collapsing the key into the digest would make a hit undecidable
// before the fetch, which is the one thing a URL-keyed cache must be able to do.
//
// A STORE TREE ALSO CONTAINS NO LOCKS (see store.go), and an entry is no exception: the fetch
// lock a URL-keyed cache wants belongs to the runtime/state root, never beside an entry.
// PutEntry must not tempt anyone into one.

// AnnotationCacheKey is the annotation that carries an entry's raw cache key. It is the SAME
// name `spec/cache` uses, deliberately: one key vocabulary across every store, so a reader that
// knows one store knows them all.
const AnnotationCacheKey = "ai.opencharly.cache.key"

// MediaTypeStoreEntry is the media type of a store entry manifest.
const MediaTypeStoreEntry = "application/vnd.opencharly.store.entry.v1+json"

// Entry is one keyed cache entry: what it IS, what KEY finds it, and where its payload lives.
//
// Exactly one of Payload or Tree is set:
//   - Payload — a single content-addressed blob; its VIEW is LinkBlob(payload, path).
//   - Tree    — a tree manifest (see tree.go); its VIEW is MaterializeTree(tree, dir), because
//     a tree has no single payload digest by construction.
type Entry struct {
	// Key is the caller's cache key: the identity of the INPUTS, not of the bytes. It is
	// opaque to the store and is never derived from the payload.
	Key string `json:"key"`
	// MediaType is what the payload IS (an RFC 6838 type the consumer understands).
	MediaType string `json:"media_type"`
	// Payload is the single payload blob's digest ("" for a tree entry).
	Payload string `json:"payload,omitempty"`
	// Tree is the tree manifest's digest ("" for a single-file entry).
	Tree string `json:"tree,omitempty"`
	// Annotations is anything else the writer wants recorded — a resolved commit, an etag, a
	// submodule-completeness flag: the RESOLUTION RECORD D-STORE-5 needs, kept beside the key
	// and outside the payload's digest on purpose (a payload must not carry a timestamp).
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PutEntry stores an entry and indexes it by its key. It returns the entry manifest's digest.
// A later PutEntry with the same key REPLACES the index pointer; the previous payload stays in
// the store as the content-addressed blob it is (immutable, and still reachable by digest).
func (s *Store) PutEntry(e Entry) (string, error) {
	if e.Key == "" {
		return "", fmt.Errorf("a store entry must declare its key (the key is the INPUT identity, not the payload digest)")
	}
	if e.MediaType == "" {
		return "", fmt.Errorf("entry %q declares no media type: a reader must know what it holds", e.Key)
	}
	if (e.Payload == "") == (e.Tree == "") {
		return "", fmt.Errorf("entry %q must name exactly one of payload or tree", e.Key)
	}
	if e.Payload != "" && !s.HasBlob(e.Payload) {
		return "", fmt.Errorf("entry %q names payload %s, which is not in the store", e.Key, e.Payload)
	}
	if e.Tree != "" {
		if _, err := s.ReadTree(e.Tree); err != nil {
			return "", fmt.Errorf("entry %q names tree %s: %w", e.Key, e.Tree, err)
		}
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	manifestDigest, err := s.PutBlob(raw)
	if err != nil {
		return "", err
	}
	if err := s.indexEntry(e.Key, manifestDigest, int64(len(raw))); err != nil {
		return "", err
	}
	return manifestDigest, nil
}

// LookupEntry resolves an entry BY KEY — one index read, no payload bytes touched and no
// directory walk, which is what lets a caller decide HIT/MISS before fetching anything.
func (s *Store) LookupEntry(key string) (*Entry, bool, error) {
	idx, err := s.readIndex()
	if err != nil {
		return nil, false, err
	}
	wantTag := entryTag(key)
	for _, d := range idx.Manifests {
		if d.Annotations[AnnotationCacheKey] != key && d.Annotations["org.opencontainers.image.ref.name"] != wantTag {
			continue
		}
		raw, err := s.ReadBlob(d.Digest)
		if err != nil {
			return nil, false, err
		}
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, false, fmt.Errorf("parse entry for key %q: %w", key, err)
		}
		return &e, true, nil
	}
	return nil, false, nil
}

// ReadEntry is LookupEntry for a caller that requires the key to exist.
func (s *Store) ReadEntry(key string) (*Entry, error) {
	e, ok, err := s.LookupEntry(key)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("store %s holds no entry for key %q", s.dir, key)
	}
	return e, nil
}

// Entries lists every keyed entry in the store, sorted by key — the cache surface's inventory.
func (s *Store) Entries() ([]Entry, error) {
	idx, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(idx.Manifests))
	for _, d := range idx.Manifests {
		raw, err := s.ReadBlob(d.Digest)
		if err != nil {
			return nil, err
		}
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("parse entry %s: %w", d.Digest, err)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// indexDescriptor is the subset of an OCI index descriptor this package reads and writes.
type indexDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type storeIndex struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	Manifests     []indexDescriptor `json:"manifests"`
}

// entryTag derives the index tag from a key. The tag must satisfy the image spec's character
// set, and a key may not (a URL hash, a colon-bearing digest), so the tag is a hash of the key
// while the RAW key rides in the annotation — the tag is the index slot, the annotation is the
// identity.
func entryTag(key string) string {
	dgst, _ := DigestOf([]byte(key))
	return "k-" + strings.TrimPrefix(dgst, "sha256:")[:32]
}

func (s *Store) readIndex() (*storeIndex, error) {
	raw, err := os.ReadFile(filepath.Join(s.dir, "index.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &storeIndex{SchemaVersion: 2, MediaType: "application/vnd.oci.image.index.v1+json"}, nil
		}
		return nil, err
	}
	var idx storeIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("parse store index: %w", err)
	}
	if idx.MediaType == "" {
		idx.MediaType = "application/vnd.oci.image.index.v1+json"
	}
	return &idx, nil
}

// indexEntry upserts an entry's descriptor in the store's index.json, writing atomically (tmp +
// rename) so a concurrent reader sees either the whole old index or the whole new one.
func (s *Store) indexEntry(key, manifestDigest string, size int64) error {
	idx, err := s.readIndex()
	if err != nil {
		return err
	}
	tag := entryTag(key)
	desc := indexDescriptor{
		MediaType:   MediaTypeStoreEntry,
		Digest:      manifestDigest,
		Size:        size,
		Annotations: map[string]string{AnnotationCacheKey: key, "org.opencontainers.image.ref.name": tag},
	}
	replaced := false
	for i, d := range idx.Manifests {
		if d.Annotations[AnnotationCacheKey] == key || d.Annotations["org.opencontainers.image.ref.name"] == tag {
			idx.Manifests[i] = desc
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Manifests = append(idx.Manifests, desc)
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	// Through the ONE atomic-write helper: this call site had its OWN fixed temp path
	// (`index.json.tmp`), so two concurrent PutEntry calls raced exactly as two PutBlob calls did.
	return writeFileAtomic(filepath.Join(s.dir, "index.json"), raw)
}
