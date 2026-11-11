// Package storekit owns charly's content-addressed STORE, and re-exports the ROOTS it lives
// under.
//
// THE ROOTS ARE THE CONTRACT MODULE'S, and this package re-exports them rather than keeping a
// second computation. D-CORE-1 has core build "the store instance (built on the sdk store,
// §17)" — the module graph is core → sdk for the store — but core may import only permitted
// contract surfaces and never an sdk mechanism kit, so a root resolved ONLY here was a root the
// host could not reach: it hand-built `$XDG_CACHE_HOME/charly` and `$XDG_RUNTIME_DIR/charly` and
// therefore could not see `CHARLY_CACHE_DIR`, which is exactly the defect
// opencharly/charly#1119 records. `github.com/opencharly/spec/storeroot` now owns the four roots
// and the single override; `Dir` and `CacheDir` below delegate to it, so every plugin that
// imports this package gets the same four roots and the same strings as before, and the host
// reaches the SAME implementation through a surface it may import.
//
// A roots or store implementation living anywhere else is still the deviation this package exists
// to end: before it, the cache root was resolved inside `spec/cache` (under the CONFIG root,
// contrary to D-STORE-1 — opencharly/spec#233) and four other packages built their own `.cache`
// paths by hand.
//
// BUILDING THIS PACKAGE BEFORE `spec`'S TAG EXISTS — the same-module trap: a `go.work` cannot
// list one module twice, so two worktrees of THIS module (`github.com/opencharly/sdk`) cannot be
// bridged by a workspace; the error is that a workspace module is listed more than once. A
// workspace CAN list this module's worktree together with a `spec` worktree (two DIFFERENT
// modules), which is the legal pre-tag route; the durable route is a branch commit of this
// package that a caller fetches by path. The pushed form carries a real `require` on the spec
// tag — never a `replace`, and never a committed `go.work`.
//
// TWO LAYERS, NEVER CONFUSED (the operator's one-store mandate):
//
//   - THE STORE is one OCI Image Layout per named store, under the ONE cache root. Every
//     cached item is ONE OCI image manifest: its config blob carries the entry's validity
//     metadata, its layers carry the payload, its annotations carry the cache key
//     (`ai.opencharly.cache.key`) and the tag (`org.opencontainers.image.ref.name`). Blobs are
//     content-addressed by digest, publication is atomic (tmp + rename), and reads take no
//     lock.
//   - A VIEW is a materialization a consumer actually reads — a tree a loader scans, a binary
//     a loader execs, a disk a hypervisor opens. Views are HARD-LINKED out of the store's own
//     blobs and live INSIDE the store tree, so they are on one filesystem by construction,
//     they add bytes exactly once, and deleting a view never orphans a blob.
//
// D-STORE-6, and the one place it must not be misread: PAYLOAD layers carry no timestamps and
// no absolute paths; the validity metadata — which necessarily contains a resolution time —
// lives in the CONFIG blob, and the config blob is deliberately NOT part of the payload
// digest. Do not "fix" this by stripping the validity record or by putting a timestamp in a
// payload.
package storekit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencharly/spec/storeroot"
)

// Root names one of the four roots — a RE-EXPORT of the contract module's type, not a second
// declaration, so a `storekit.Root` value IS a `storeroot.Root` value and the two can never
// drift into different root sets.
type Root = storeroot.Root

const (
	// RootConfig — `$XDG_CONFIG_HOME/charly`: what the USER sets.
	RootConfig = storeroot.RootConfig
	// RootCache — `$XDG_CACHE_HOME/charly`: everything content-addressed and re-derivable.
	// The single `CHARLY_CACHE_DIR` override relocates this one and therefore every store.
	RootCache = storeroot.RootCache
	// RootState — `$XDG_STATE_HOME/charly`: durable state that is NOT cache — install
	// ledgers, process locks, anything a restart must not lose and a `cache clear` must not
	// touch.
	RootState = storeroot.RootState
	// RootRuntime — `$XDG_RUNTIME_DIR/charly`: per-boot runtime state.
	RootRuntime = storeroot.RootRuntime
)

// Dir resolves one of the four roots. It is THE resolver — no caller builds a `.cache` path by
// hand — and it resolves through the contract module (`spec/storeroot`), so the host and every
// plugin get the same answer from the same code.
func Dir(r Root) (string, error) { return storeroot.Dir(r) }

// CacheDir resolves the cache root: `$XDG_CACHE_HOME/charly`, or `CHARLY_CACHE_DIR` when set.
// Every named store lives under it.
func CacheDir() (string, error) { return storeroot.CacheDir() }

// StoreDir returns the directory of the named store `name` under the cache root. A store name
// is a single path element: it may not contain a separator, so a store can never escape the
// root.
//
// This is store ADDRESSING, not root resolution — it joins a name onto the ONE cache root — so
// it stays beside the store it addresses, while the root itself comes from the contract module.
func StoreDir(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, filepath.Separator) || name == "." || name == ".." {
		return "", fmt.Errorf("invalid store name %q", name)
	}
	root, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

// NamedStores lists every named store under the cache root, sorted. A store is a directory
// that LOOKS like an OCI image layout (it carries the `oci-layout` marker), so unrelated state
// under the root — lock directories, transient trees — is never mistaken for a store. A
// missing root is an empty list, never an error.
func NamedStores() ([]string, error) {
	root, err := CacheDir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if _, lerr := os.Stat(filepath.Join(root, e.Name(), LayoutFile)); lerr != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}
