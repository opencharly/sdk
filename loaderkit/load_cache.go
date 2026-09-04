package loaderkit

// load_cache.go — the host-side MATERIALIZED-TREE cache (the 32-lane oversubscription stall
// root fix, SIGQUIT goroutine dumps 2026-09-05).
//
// ROOT. Every "charly fleet add" / "charly check live" / rebuild phase spawns a CLI
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
// every consumer, and the shared on-disk cache under ~/.cache/charly is what lets the wave's
// separate OS processes reuse ONE another's work.
//
// KEY (the project config state + the resolved refs' hashes, content-addressed). The key is a
// SHA-256 over (1) the deterministic JSON serialization of the spec.LoadedProject walk envelope —
// the FULL materialize input: root config, directive import:/repo: pins, flat imports,
// discovered manifests and every mounted namespace, i.e. the CONTENT of every ref the walk
// resolved (fetched pinned repos are walked into the envelope, CanonicalRef → EnsureRepoDownloaded
// → parsed docs/manifests) — and (2) the compiled schema CalVer (kit.LatestSchemaVersion). Any
// config change, ref re-pin, branch advance, or fetched-content change alters the envelope → a new
// key (the invalidation rule: any config/ref change = a new key). Hashing the envelope is
// strictly stronger than a bare list of resolved commit hashes — it is a content hash of the
// resolved refs, so a re-pin whose content is byte-identical (and whose materialization therefore
// IS identical) correctly does NOT re-key, while a re-pin to different content re-keys by the
// content itself; the schema-CalVer component makes a binary schema upgrade re-key too.
//
// TTL. materializeCacheTTL (1h) bounds the only materialize input the envelope cannot see: a
// provider-set / loader-logic change in the embedding binary that ships WITHOUT a schema CalVer
// bump. A stale entry is skipped (miss → re-materialize), never served.
//
// LAYOUT + CONCURRENCY (the spec/refs git-cache patterns). One entry file per key,
// ~/.cache/charly/materialized/<sha256-hex>.json (CHARLY_MATERIALIZED_CACHE overrides the root,
// mirroring CHARLY_REPO_CACHE), holding {resolved RFC3339, tree = MarshalMaterialized bytes}.
// Reads are LOCK-FREE: a writer publishes atomically (tmp + rename), so a reader sees either the
// complete old state (miss) or the complete new one (hit) — never a torn entry (the
// downloadRepoFrom publish pattern). The MISS path takes a per-key blocking advisory flock
// (spec/lock.AcquireFileLock, the downloadRepoFrom per-cache-path pattern) and DOUBLE-CHECKS under
// the lock, so the wave's first child materializes once and concurrent first-missers reuse its
// entry instead of re-running the unify. Every cache failure — a key error, a read error, lock
// contention timeout, a corrupt entry — degrades to a DIRECT materialize: the cache is an
// optimization, never a correctness dependency.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/lock"
	"github.com/opencharly/spec/spec"
)

// materializedCacheEnvName overrides the materialized-tree cache root (tests isolate to a
// temporary dir; operators can point it anywhere), mirroring CHARLY_REPO_CACHE's role for the
// repo cache.
const materializedCacheEnvName = "CHARLY_MATERIALIZED_CACHE"

var (
	// materializeCacheTTL bounds how long a cached materialization is trusted before it counts as
	// a miss and the unify re-runs. A package var (not a const) so a test can shorten it, exactly
	// like spec/lock's lockTimeout; the git-cache precedent in spec/refs (submoduleCacheTTL) is 1h.
	materializeCacheTTL = time.Hour
	// materializedTreeCacheEnabled is the escape hatch + test hook: when false every load
	// materializes directly, exactly as before the cache existed. Tests flip it to demonstrate the
	// no-cache behavior (the materializer is called once per load).
	materializedTreeCacheEnabled = true
)

// materializedCacheDir returns the materialized-tree cache root: $CHARLY_MATERIALIZED_CACHE if
// set, else ~/.cache/charly/materialized (a sibling of the refs repo cache,
// ~/.cache/charly/repos — RepoCacheDir). Mirrors spec/refs/cache.go's RepoCacheDir shape.
func materializedCacheDir() (string, error) {
	if envDir := os.Getenv(materializedCacheEnvName); envDir != "" {
		return envDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("materialized-tree cache: home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "charly", "materialized"), nil
}

// loadedProjectCacheKey derives the cache key for ONE materialize input: a SHA-256 over (1) the
// deterministic JSON of the walk envelope (the project config state after resolution — see the
// file header: the envelope EMBEDS every resolved ref's content, so a content hash of it is the
// resolved-refs-hash marker, stronger than a bare commit list) and (2) the embedding binary's
// compiled schema CalVer, so a schema upgrade re-keys. An error (an un-marshalable envelope) means
// the cache cannot participate — callers fall back to a direct materialize.
func loadedProjectCacheKey(lp *spec.LoadedProject) (string, error) {
	env, err := json.Marshal(lp)
	if err != nil {
		return "", fmt.Errorf("materialized-tree cache key: encode walk envelope: %w", err)
	}
	h := sha256.New()
	_, _ = h.Write(env)
	_, _ = h.Write([]byte{0}) // field separator (both halves are length-delimited; the marker keeps the framing explicit)
	_, _ = h.Write([]byte(kit.LatestSchemaVersion().String()))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// materializedCacheEntry is one cache file's contents: when the materialization was resolved and
// the MarshalMaterialized bytes of the merged tree (the SAME wire envelope the loader-materialize
// leg returns, PluginKinds preserved — never a plain json.Marshal).
type materializedCacheEntry struct {
	Resolved time.Time `json:"resolved"`
	Tree     []byte    `json:"tree"`
}

// readMaterializedTree returns the cached marshal-materialized tree for key when present and
// fresh (TTL-wise), else (nil, false). Every failure — absent file, corrupt JSON, empty tree,
// stale TTL, disabled cache — is a miss; callers re-materialize. Lock-free: the writer's atomic
// rename guarantees a complete read or an absent one.
func readMaterializedTree(key string) ([]byte, bool) {
	if !materializedTreeCacheEnabled {
		return nil, false
	}
	dir, err := materializedCacheDir()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return nil, false
	}
	var e materializedCacheEntry
	if json.Unmarshal(data, &e) != nil || len(e.Tree) == 0 {
		return nil, false
	}
	if time.Since(e.Resolved) > materializeCacheTTL {
		return nil, false
	}
	return e.Tree, true
}

// writeMaterializedTree persists a tree under key, atomically (tmp + rename so a concurrent
// lock-free reader never sees a torn entry) and best-effort (a write failure is silent — the
// cache is an optimization). Publishes only outside-materialize state: the entry's Resolved is
// now, so the entry is fresh for materializeCacheTTL.
func writeMaterializedTree(key string, tree []byte) {
	if !materializedTreeCacheEnabled || len(tree) == 0 {
		return
	}
	dir, err := materializedCacheDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	e := materializedCacheEntry{Resolved: time.Now(), Tree: tree}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	final := filepath.Join(dir, key+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, final)
	sweepMaterializedCache(dir)
}

// sweepMaterializedCache opportunistically removes entry files older than 2x the TTL so a heavy
// host (many distinct project states per hour) stays bounded without a cleanup task. A file the
// sweep removes is already TTL-stale, so no live entry is ever lost. Best-effort: errors are
// swallowed.
func sweepMaterializedCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-2 * materializeCacheTTL)
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, de.Name()))
		}
	}
}

// materializedCacheLockPath is the per-key advisory-lock path (a locks/ sibling keeps the entries
// dir sweepable).
func materializedCacheLockPath(key string) (string, error) {
	dir, err := materializedCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "locks", key+".lock"), nil
}

// MaterializeLoadedProjectCached wraps the registry-coupled materialize leg with the host-side
// materialized-tree cache: same-state loads reuse the stored merged tree instead of re-running
// the CUE unify. It is the cache's seam-shaped face, wired by LoadSeamsFromExecutor around
// loader-executor MaterializeLoadedProject legs (compiled-in host AND out-of-process executor)
// — see the file header for the root + design. materialize must have the LoaderExecutor
// contract's semantics: fill merged (and the byID registration) from lp.
//
// Failure contract: the cache NEVER fails a load. A key error, cache-dir error, corrupt entry or
// lock-contention timeout degrades to materialize(lp, merged, byID) exactly as if the cache did
// not exist; a hit that fails to decode is re-materialized.
func MaterializeLoadedProjectCached(lp *spec.LoadedProject, merged *spec.UnifiedFile, byID map[int64]*spec.UnifiedFile, materialize func(lp *spec.LoadedProject, merged *spec.UnifiedFile, byID map[int64]*spec.UnifiedFile) error) error {
	if !materializedTreeCacheEnabled || lp == nil {
		// Disabled cache → every load materializes directly, exactly as before the cache
		// existed. A nil envelope would hash to a bogus "null" key — also degrade to direct
		// (a nil lp panics in the real seam anyway, so nothing here can be cached).
		return materialize(lp, merged, byID)
	}
	key, kerr := loadedProjectCacheKey(lp)
	if kerr != nil {
		return materialize(lp, merged, byID)
	}
	if tree, ok := readMaterializedTree(key); ok {
		if err := UnmarshalMaterialized(tree, merged); err == nil {
			// Reproduce MaterializeLoadedProject's first step — registering THIS project's merged
			// tree under its walk-assigned id — so a caller that keeps byID observes the same
			// pointer registration a real materialize would perform.
			if lp.ID != 0 {
				byID[lp.ID] = merged
			}
			return nil
		}
		// Corrupt cached bytes: fall through and re-materialize.
	}
	// MISS: serialize the materialize under the per-key lock (a flock — never a retry), then
	// double-check so a concurrent first materializer's entry is reused rather than recomputed.
	lockPath, lerr := materializedCacheLockPath(key)
	if lerr != nil {
		return materialize(lp, merged, byID)
	}
	release, lerr := lock.AcquireFileLock(lockPath, true)
	if lerr != nil {
		// Lock contention timeout (or an IO failure) — degrade to a direct materialize; the
		// cache is an optimization, never a correctness dependency.
		return materialize(lp, merged, byID)
	}
	defer func() { _ = release() }()
	if tree, ok := readMaterializedTree(key); ok {
		if err := UnmarshalMaterialized(tree, merged); err == nil {
			if lp.ID != 0 {
				byID[lp.ID] = merged
			}
			return nil
		}
	}
	if err := materialize(lp, merged, byID); err != nil {
		return err
	}
	if tree, merr := MarshalMaterialized(merged); merr == nil {
		writeMaterializedTree(key, tree)
	}
	return nil
}
