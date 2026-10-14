// Package storekit owns charly's storage ROOTS and its content-addressed STORE.
//
// DESIGN puts both here: D-SDK-1 lists "the store and its roots (§17)" among the shared
// mechanisms an sdk package owns, and D-CORE-1 has core build "the store instance (built on
// the sdk store, §17)" — i.e. the module graph is core → sdk for exactly this. A roots or
// store implementation living anywhere else is the deviation this package exists to end:
// before it, the cache root was resolved inside `spec/cache` (under the CONFIG root, contrary
// to D-STORE-1) and four other packages built their own `.cache` paths by hand.
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
)

// Root names one of the four D-STORE-1 roots.
type Root string

const (
	// RootConfig — `$XDG_CONFIG_HOME/charly`: what the USER sets.
	RootConfig Root = "config"
	// RootCache — `$XDG_CACHE_HOME/charly`: everything content-addressed and re-derivable.
	// The single `CHARLY_CACHE_DIR` override relocates this one and therefore every store.
	RootCache Root = "cache"
	// RootState — `$XDG_STATE_HOME/charly`: durable state that is NOT cache — install
	// ledgers, process locks, anything a restart must not lose and a `cache clear` must not
	// touch.
	RootState Root = "state"
	// RootRuntime — `$XDG_RUNTIME_DIR/charly`: per-boot runtime state.
	RootRuntime Root = "runtime"
)

// Dir resolves one of the four roots. It is THE resolver: no caller builds a `.cache` path by
// hand, and there is exactly one environment override in the whole system
// (`CHARLY_CACHE_DIR`, on the cache root — TODO 7.2 counts the extra per-cache variables this
// replaces, and they are being deleted one per migration).
func Dir(r Root) (string, error) {
	switch r {
	case RootCache:
		return CacheDir()
	case RootConfig:
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve config root: %w", err)
		}
		return filepath.Join(base, "charly"), nil
	case RootState:
		if v := os.Getenv("XDG_STATE_HOME"); v != "" {
			return filepath.Join(v, "charly"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve state root: %w", err)
		}
		return filepath.Join(home, ".local", "state", "charly"), nil
	case RootRuntime:
		if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
			return filepath.Join(v, "charly"), nil
		}
		// No runtime dir (a container, a cron job): fall back to the STATE root rather than
		// /tmp, which is shared and world-writable. Documented, not silent.
		return Dir(RootState)
	default:
		return "", fmt.Errorf("unknown root %q (want %s|%s|%s|%s)", r, RootConfig, RootCache, RootState, RootRuntime)
	}
}

// CacheDir resolves the cache root: `$XDG_CACHE_HOME/charly`, or `CHARLY_CACHE_DIR` when set.
// Every named store lives under it.
func CacheDir() (string, error) {
	if root := os.Getenv("CHARLY_CACHE_DIR"); root != "" {
		return root, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache root: %w", err)
	}
	return filepath.Join(base, "charly"), nil
}

// StoreDir returns the directory of the named store `name` under the cache root. A store name
// is a single path element: it may not contain a separator, so a store can never escape the
// root.
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
