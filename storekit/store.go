package storekit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A STORE TREE CONTAINS NO LOCKS. Measured on a live store (the wave lead's recon): one store
// directory carried a `.lock` file and a populated `locks/` tree beside its `oci-layout` and
// `blobs/`. That is a cache holding a lock, and the homes rule this codebase adopted is
// explicit: a lock is transient runtime state with no content address — a digest-keyed lock
// could even be SHARED between two processes that must not share it — so it belongs to the
// state root (host-global arbitration) or the runtime root (per-boot), never in a store.
//
// This mechanism therefore never places a lock inside a store's tree, and NamedStores()'s
// layout-marker test exists so a stray `locks/` directory under the cache root is never
// mistaken for a store. Where the CURRENT `spec/cache` store does place one, that is that
// store's debt (opencharly/spec#233), not something a consumer should work around.
//
// Store is ONE named OCI image layout under the cache root. Its `blobs/<alg>/<digest>` files
// ARE the cached bytes — the only copy of them anywhere on the host — and every consumer reads
// either a blob directly or a VIEW hard-linked from it.
type Store struct {
	dir string
}

// LayoutFile is the marker that makes a directory an OCI image layout, and therefore a store.
const LayoutFile = "oci-layout"

// blobDir is the content-addressed blob directory inside a store.
const blobDir = "blobs"

// OpenStoreDir opens the store rooted at dir (an existing layout, or a path a pull will
// populate).
func OpenStoreDir(dir string) *Store { return &Store{dir: dir} }

// OpenStore opens the named store, resolving its path through the ONE cache root.
func OpenStore(name string) (*Store, error) {
	dir, err := StoreDir(name)
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir is the store's layout directory.
func (s *Store) Dir() string { return s.dir }

// blobPath is the on-disk path of a content-addressed blob. It validates the digest's SHAPE
// before building the path: a digest is a path element here, so a malformed one must not be
// able to escape `blobs/`.
func (s *Store) blobPath(dgst string) (string, error) {
	alg, hexPart, ok := strings.Cut(dgst, ":")
	if !ok || alg == "" || hexPart == "" {
		return "", fmt.Errorf("malformed digest %q", dgst)
	}
	if strings.ContainsAny(alg+hexPart, `/\`) || strings.Contains(hexPart, "..") {
		return "", fmt.Errorf("malformed digest %q", dgst)
	}
	return filepath.Join(s.dir, blobDir, alg, hexPart), nil
}

// BlobPath returns the store's path for a content-addressed blob, so a caller can ASSERT that a view
// shares the store's bytes (`SameFile(store.BlobPath(d), viewPath)`) instead of trusting it.
//
// There is deliberately NO ChmodBlob: a view is a HARDLINK, so chmod-ing the view changes the shared
// inode — the caller does `os.Chmod(viewPath, mode)` and the store stays out of file modes. Mode is
// not part of a digest, which is exactly why it is the consumer's concern and not an entry field.
func (s *Store) BlobPath(dgst string) (string, error) { return s.blobPath(dgst) }

// HasBlob reports whether the store already holds a blob by digest — the primitive every
// caller needs BEFORE it does any work: a digest that is present is never fetched, copied or
// rebuilt.
func (s *Store) HasBlob(dgst string) bool {
	p, err := s.blobPath(dgst)
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ReadBlob returns a blob's bytes, VERIFIED against the digest they are addressed by. Content
// addressing is the point: it is what makes a store entry immutable and a cache hit safe.
func (s *Store) ReadBlob(dgst string) ([]byte, error) {
	p, err := s.blobPath(dgst)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", dgst, err)
	}
	got, err := DigestOf(data)
	if err != nil {
		return nil, err
	}
	if got != dgst {
		return nil, fmt.Errorf("blob %s does not match its content address (got %s)", dgst, got)
	}
	return data, nil
}

// PutBlob writes bytes into the store as a content-addressed blob and returns their digest. A
// blob that is already present is left untouched (the same bytes are the same file), and the
// write is atomic — tmp + rename — so a concurrent reader never sees a partial blob.
func (s *Store) PutBlob(data []byte) (string, error) {
	dgst, err := DigestOf(data)
	if err != nil {
		return "", err
	}
	p, err := s.blobPath(dgst)
	if err != nil {
		return "", err
	}
	if s.HasBlob(dgst) {
		return dgst, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dgst, nil
}

// Blobs lists the digests the store holds, sorted.
func (s *Store) Blobs() ([]string, error) {
	var out []string
	algs, err := os.ReadDir(filepath.Join(s.dir, blobDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, alg := range algs {
		if !alg.IsDir() {
			continue
		}
		ents, err := os.ReadDir(filepath.Join(s.dir, blobDir, alg.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
				continue
			}
			out = append(out, alg.Name()+":"+e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// LinkBlob materializes a VIEW of a stored blob at dest: a HARDLINK, never a copy.
//
// There is deliberately NO fallback. A copy would be a second set of bytes (the operator's
// mandate is "cached once"), and a cross-device fallback would hide a store that is not one
// filesystem — the single way the property could silently stop being true. A link failure is
// therefore a hard error that names the reason.
func (s *Store) LinkBlob(dgst, dest string) error {
	src, err := s.blobPath(dgst)
	if err != nil {
		return err
	}
	if !s.HasBlob(dgst) {
		return fmt.Errorf("blob %s is not in the store %s", dgst, s.dir)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Link(src, dest); err != nil {
		return fmt.Errorf("hardlink %s -> %s: %w (a view must share the store's bytes; a copy is not an option and there is no cross-device fallback)", src, dest, err)
	}
	return nil
}

// SameFile reports whether two paths are the same file — the measurable form of "the view and
// the blob are one set of bytes". Callers ASSERT this rather than assuming it.
func SameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}

// DigestOf renders the sha256 content address of a byte slice, in the `sha256:<hex>` form the
// store and the artifact annotations use.
func DigestOf(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
