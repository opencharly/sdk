package storekit

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// tree.go — DIRECTORY payloads in the store.
//
// A cache payload is not always one file. A BuildKit cache export is a directory, a repo
// checkout is a tree, and a disk image is one large opaque file — so the store must express all
// three without a special case, and without ever keeping two copies of the bytes.
//
// The ENTRY MODEL is the OCI model and needs no exception: a payload is blobs, and a blob may
// carry any RFC 6838 media type. What a single-file entry cannot do is MATERIALIZE a directory
// — so a tree is stored as per-file blobs plus ONE manifest blob that names the tree's own
// media type, and its view is reconstructed as a tree of HARDLINKS. That keeps the operator's
// "bytes once" property true for directories too:
//
//   - a TAR of the directory would be one blob, and extracting it would double the bytes on
//     disk (the tar plus the extracted tree) — right for a genuinely opaque payload, wrong for
//     a cache the consumer reads as a directory;
//   - per-file blobs plus hard-linked reconstruction store each file once and add only
//     directory entries, exactly like the single-file view.
//
// The manifest is itself a blob, so it is content-addressed and immutable like everything else,
// and it carries the tree's media type so a consumer knows what it holds without guessing.

// TreeFile is one file inside a stored tree.
type TreeFile struct {
	// Digest is the file's content address (its blob in this store).
	Digest string `json:"digest"`
	// Mode is the file's permission bits. Mode is NOT part of a digest, so it travels here
	// and is re-applied on materialization.
	Mode uint32 `json:"mode"`
}

// TreeEntry is a stored directory payload: every file by content address, plus the media type
// that says what the tree IS (e.g. a BuildKit cache export, a repo view).
type TreeEntry struct {
	// MediaType is the tree's own RFC 6838 media type — never a bare "tar" by convention.
	MediaType string `json:"media_type"`
	// Files maps a slash-separated RELATIVE path to its blob and mode.
	Files map[string]TreeFile `json:"files"`
}

// PutTree stores a directory as a tree entry and returns the digest of its MANIFEST blob (the
// entry's identity). Every file is a content-addressed blob, so identical files across trees —
// and across releases — are stored once; a file already present is not rewritten.
func (s *Store) PutTree(dir, mediaType string) (string, error) {
	if mediaType == "" {
		return "", fmt.Errorf("a stored tree must declare its media type (the entry's identity)")
	}
	entry := TreeEntry{MediaType: mediaType, Files: map[string]TreeFile{}}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dgst, err := s.PutBlob(data)
		if err != nil {
			return err
		}
		entry.Files[filepath.ToSlash(rel)] = TreeFile{Digest: dgst, Mode: uint32(info.Mode().Perm())}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("store tree %s: %w", dir, err)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	return s.PutBlob(raw)
}

// ReadTree decodes a tree entry's manifest.
func (s *Store) ReadTree(entryDigest string) (*TreeEntry, error) {
	raw, err := s.ReadBlob(entryDigest)
	if err != nil {
		return nil, err
	}
	var entry TreeEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse tree entry %s: %w", entryDigest, err)
	}
	if entry.MediaType == "" {
		return nil, fmt.Errorf("tree entry %s declares no media type", entryDigest)
	}
	return &entry, nil
}

// MaterializeTree reconstructs a stored tree at dest as a tree of HARDLINKS to the store's
// blobs. Nothing is copied, and the file set is created deterministically (paths sorted) so a
// partial failure is reproducible rather than order-dependent.
func (s *Store) MaterializeTree(entryDigest, dest string) error {
	entry, err := s.ReadTree(entryDigest)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(entry.Files))
	for rel := range entry.Files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		f := entry.Files[rel]
		// A stored path is data: it must not escape the destination.
		clean := filepath.Clean(filepath.FromSlash(rel))
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("tree entry %s names an unsafe path %q", entryDigest, rel)
		}
		target := filepath.Join(dest, clean)
		if err := s.LinkBlob(f.Digest, target); err != nil {
			return fmt.Errorf("materialize %s: %w", rel, err)
		}
		if err := os.Chmod(target, os.FileMode(f.Mode)); err != nil {
			return err
		}
	}
	return nil
}
