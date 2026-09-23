package loaderkit

// load_cache.go — the host-side MATERIALIZED-TREE cache (the 32-lane oversubscription stall
// root fix, SIGQUIT goroutine dumps 2026-09-05).
//
// ROOT. Every "charly deploy add" / "charly check live" / rebuild phase spawns a CLI
// subcommand child that RE-MATERIALIZES the full merged deploy tree through CUE:
// ResolveMergedDeployTreeViaExecutor → MaterializeLoadedProject → the CUE disjunction unify over
// the project's pinned-repo tree (goroutine 1 [runnable] inside
// cuelang.org/go/cue/.../doDisjunct/crossProduct/processDisjunctions). Under 32-lane
// oversubscription every lane's child burns CPU in the unify, the unify exceeds its budget, and
// the parent's os/exec.Cmd.Run (host_build_cli.go via executorReverseServer.HostBuild) waits
// forever. The unify's OUTPUT — the merged spec.UnifiedFile — is a DETERMINISTIC function of the
// walk envelope (spec.LoadedProject) plus the embedding binary's compiled schema/registry, so the
// wave's later children can reuse the first child's materialization instead of re-running the
// unify.
//
// HOOK. The cache wraps the MaterializeLoadedProject SEAM inside LoadSeamsFromExecutor
// (load_executor.go) — the ONE seam constructor BOTH loader placements drive loaderkit.LoadUnified
// through (the compiled-in host placement, hostLoaderExecutor — the CLI subcommand child's own
// load — and every genuine out-of-process plugin over Executor.HostBuild). One change point covers
// every consumer, and the shared on-disk Store under the charly cache dir is what lets the wave's
// separate OS processes reuse ONE another's work.
//
// STORAGE. The mechanism is the ONE shared spec/cache ArtifactStore (R3): an OCI
// Image Layout (oci-layout + index.json + blobs/<alg>/<digest>) — lock-free
// atomic reads, a per-key flock + double-check on the miss path (Fill), and
// Docker-style prune by entry cap. This file OWNS only the materialize-specific
// inputs — the KEY (the components digest) and the tree wire codec.
//
// KEY (the project config state + the resolved refs' hashes, content-addressed). The key is the
// components digest of three inputs: (1) the walk envelope's SHA-256 — the FULL materialize input:
// root config, directive import:/repo: pins, flat imports, discovered manifests and every mounted
// namespace, i.e. the CONTENT of every ref the walk resolved (fetched pinned repos are walked into
// the envelope, CanonicalRef → EnsureRepoDownloaded → parsed docs/manifests); (2) the compiled
// schema CalVer (kit.LatestSchemaVersion); (3) the compiled LOADER logic identity (the sdk module
// version — loaderkit's parse/fold changes ship in a new sdk version). Any config change, ref
// re-pin, branch advance, fetched-content change, schema bump, or loader-logic change re-keys. The
// entry RECORDS the components too, so the read refuses an entry whose components differ from the
// current ones (self-detection). There is NO time validity (the Docker cache rule): an old entry
// whose components match is served; reclamation of stale-input orphans is the Store prune's
// storage-only job.
//
// Freshness is content, not VCS state: a local replace target under development carries uncommitted
// edits, so hashing the tree CONTENT (the envelope carries every resolved ref's bytes) catches a
// bump where a git-HEAD-keyed stamp would serve a stale binary. The sdk + spec contract modules
// resolve from the proxy at pinned require versions, which the envelope's content hash covers.

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/cache"
	"github.com/opencharly/spec/spec"
)

// materializedCacheEnvName overrides the materialized-tree cache root (tests isolate to a
// temporary dir; operators can point it anywhere), mirroring CHARLY_REPO_CACHE's role for the
// repo cache.
const materializedCacheEnvName = "CHARLY_MATERIALIZED_CACHE"

var (
	// materializedCacheMaxEntries bounds the cache SIZE (the Docker builder --keep-storage
	// analogue): the Store's write path opportunistically reclaims the oldest entries beyond the
	// cap. Reclamation is STORAGE-bound only — validity is purely component-based (valid while the
	// charly.yml content + schema + loader identity are unchanged; ANY change re-materializes and a
	// new entry is written automatically — no manual invalidation, no time expiry — the Docker
	// cache rule).
	materializedCacheMaxEntries = 32
	// materializedTreeCacheEnabled is the escape hatch + test hook: when false every load
	// materializes directly, exactly as before the cache existed. Tests flip it to demonstrate the
	// no-cache behavior (the materializer is called once per load).
	materializedTreeCacheEnabled = true
)

// materializedCacheDir returns the materialized-tree cache root: $CHARLY_MATERIALIZED_CACHE if
// set, else the shared cache's `materialized` store under the charly dir
// (~/.config/charly/cache/materialized).
func materializedCacheDir() (string, error) {
	if envDir := os.Getenv(materializedCacheEnvName); envDir != "" {
		return envDir, nil
	}
	return cache.StoreDir("materialized")
}

// materializedStore opens the shared Store for the materialized tree (the ONE cache mechanism).
func materializedStore() *cache.Layout {
	dir, err := materializedCacheDir()
	if err != nil {
		return cache.OpenLayout("")
	}
	return cache.OpenLayoutLimited(dir, materializedCacheMaxEntries)
}

// loadedProjectCacheKey derives the Store key (the components digest) AND the components
// themselves (the same three values, recorded in the entry for the read-time drift check). An
// error (an un-marshalable envelope) means the cache cannot participate — callers fall back to a
// direct materialize.
func loadedProjectCacheKey(lp *spec.LoadedProject) (string, map[string]string, error) {
	env, err := json.Marshal(lp)
	if err != nil {
		return "", nil, fmt.Errorf("materialized-tree cache key: encode walk envelope: %w", err)
	}
	comps := map[string]string{
		"config_hash":     cache.HashHex(string(env)),
		"schema_calver":   kit.LatestSchemaVersion().String(),
		"loader_identity": loaderIdentity(),
	}
	return cache.KeyDigest(comps), comps, nil
}

// loaderIdentity names the COMPILED loader logic in the cache key (RCA 2026-09-06: the Phase 3
// from: name:tag split landed in loaderkit WITHOUT a schema-CalVer bump, so the key (envelope +
// schema CalVer) did not change and the cache served the PRE-SPLIT tree for up to the same-key
// interval — the exact gap the RCA measured). The identity is the sdk module version from the
// build info: loaderkit's parse/fold logic lives in the sdk module, so a loader-logic change
// necessarily ships in a NEW sdk version → a new identity → a new cache key. A new binary never
// inherits an older logic's tree. Fallback (no build info, e.g. go test): "bare" so tests in one
// binary share one namespace. loaderIdentityFn is a package var (not a const) so tests inject a
// DIFFERENT identity and prove both the key and the read-time drift detection respond to it.
var loaderIdentityFn = func() string {
	return loaderIdentityImpl()
}

func loaderIdentity() string { return loaderIdentityFn() }

func loaderIdentityImpl() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "bare"
	}
	for _, m := range info.Deps {
		if m.Path == sdkModulePath {
			return "sdk@" + m.Version
		}
	}
	return "bare"
}

// sdkModulePath is loaderkit's own module path — the identity key is the module that OWNS this
// file's logic (any rename here must rename this constant with it).
const sdkModulePath = "github.com/opencharly/sdk"

// MaterializeLoadedProjectCached wraps the registry-coupled materialize leg with the host-side
// materialized-tree cache: same-state loads reuse the stored merged tree instead of re-running the
// CUE unify. It is the cache's seam-shaped face, wired by LoadSeamsFromExecutor around
// loader-executor MaterializeLoadedProject legs (compiled-in host AND out-of-process executor) —
// see the file header for the root + design. materialize must have the LoaderExecutor
// contract's semantics: fill merged (and the byID registration) from lp.
//
// Failure contract: the cache NEVER fails a load. A key error, a corrupt entry, or a marshal
// failure degrades to materialize(lp, merged, byID) exactly as if the cache did not exist, and a
// hit that fails to decode is re-materialized. Store-INTERNAL failures (an inert store, a lock
// timeout, an IO error) never reach here either: Store.Fill degrades them to computing the value
// directly, so the ONLY error Fill returns is the materialize callback's own — which is the real
// load error and is propagated.
func MaterializeLoadedProjectCached(lp *spec.LoadedProject, merged *spec.UnifiedFile, byID map[int64]*spec.UnifiedFile, materialize func(lp *spec.LoadedProject, merged *spec.UnifiedFile, byID map[int64]*spec.UnifiedFile) error) error {
	if !materializedTreeCacheEnabled || lp == nil {
		// Disabled cache → every load materializes directly, exactly as before the cache
		// existed. A nil envelope would key to a bogus "null" entry — also degrade to direct
		// (a nil lp panics in the real seam anyway, so nothing here can be cached).
		return materialize(lp, merged, byID)
	}
	key, comps, kerr := loadedProjectCacheKey(lp)
	if kerr != nil {
		return materialize(lp, merged, byID)
	}
	// materialized records whether the miss path filled `merged` directly (the original,
	// no-round-trip semantics): the callback materializes straight into the caller's `merged`
	// under the Store's per-key lock, so a cold load pays NO extra marshal/unmarshal, and a hit
	// decodes the stored tree.
	materialized := false
	e, ferr := materializedStore().Fill(key, func() (cache.Entry, error) {
		if err := materialize(lp, merged, byID); err != nil {
			return cache.Entry{}, err
		}
		materialized = true
		tree, merr := MarshalMaterialized(merged)
		if merr != nil {
			// A marshal failure is not a load failure: return an empty entry (the Store stores
			// it, but the len check below re-materializes each load) so the caller keeps the
			// directly-materialized `merged`.
			return cache.Entry{}, nil
		}
		return cache.Entry{Payload: tree, Components: comps}, nil
	})
	if ferr != nil {
		return ferr
	}
	if materialized {
		return nil
	}
	if err := UnmarshalMaterialized(e.Payload, merged); err != nil {
		// An empty (marshal-degrade) or corrupt entry: re-materialize directly.
		return materialize(lp, merged, byID)
	}
	// Reproduce MaterializeLoadedProject's first step — registering THIS project's merged tree
	// under its walk-assigned id — so a caller that keeps byID observes the same pointer
	// registration a real materialize would perform.
	if lp.ID != 0 {
		byID[lp.ID] = merged
	}
	return nil
}
