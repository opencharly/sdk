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
// content itself; the schema-CalVer component makes a binary schema upgrade re-key too; the
// loader-identity component (below) makes a loader-logic change re-key too — so the cache is
// VALID AS LONG AS THE INPUTS (charły.yml content + schema + loader) ARE UNCHANGED, and any
// input change re-materializes + writes the new entry automatically. There is NO time validity
// (the Docker cache rule): an old entry whose components match is served; reclamation of
// stale-input orphans is the write-time prune's storage-only job.
//
// LAYOUT + CONCURRENCY (the spec/refs git-cache patterns). One entry file per key,
// ~/.cache/charly/materialized/<sha256-hex>.json (CHARLY_MATERIALIZED_CACHE overrides the root,
// mirroring CHARLY_REPO_CACHE), holding {resolved RFC3339 (reclamation data), tree = MarshalMaterialized bytes}.
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
	"runtime/debug"
	"sort"
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
	// materializedCacheMaxEntries bounds the cache SIZE (the Docker builder --keep-storage
	// analogue): the write path opportunistically reclaims the oldest entries beyond the cap.
	// Reclamation is STORAGE-bound only — validity is purely component-based (valid while the
	// charly.yml content + schema + loader identity are unchanged; ANY change re-materializes
	// and a new entry is written automatically — no manual invalidation, no time expiry — the
	// Docker cache rule).
	materializedCacheMaxEntries = 32
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

// materializeCacheComponents names the three inputs the materialized tree is a FUNCTION of:
// (1) the project CONFIG state (the content-addressed walk envelope — the resolved refs' bytes are
// EMBEDDED in it), (2) the compiled SCHEMA CalVer, (3) the compiled LOADER logic identity (the sdk
// module version — loaderkit's parse/fold changes ship in a new sdk version). The component set is
// the cache's self-detection contract: the entry records the components it was materialized under,
// and the read REFUSES an entry whose components differ from the current ones (a config/schema/
// loader change) — the miss then re-materializes and the write UPDATES the cache automatically.
// No manual invalidation is ever needed.
type materializeCacheComponents struct {
	ConfigHash     string `json:"config_hash"`
	SchemaCalVer   string `json:"schema_calver"`
	LoaderIdentity string `json:"loader_identity"`
}

// loadedProjectCacheKey derives the cache KEY (the on-disk filename: a SHA-256 over the three
// components, so each distinct configuration gets its own entry) AND the components themselves
// (the same three values, separated for the read-time drift check). An error (an un-marshalable
// envelope) means the cache cannot participate — callers fall back to a direct materialize.
func loadedProjectCacheKey(lp *spec.LoadedProject) (string, materializeCacheComponents, error) {
	env, err := json.Marshal(lp)
	if err != nil {
		return "", materializeCacheComponents{}, fmt.Errorf("materialized-tree cache key: encode walk envelope: %w", err)
	}
	envHash := sha256Hex(env)
	calver := kit.LatestSchemaVersion().String()
	identity := loaderIdentity()
	comps := materializeCacheComponents{ConfigHash: envHash, SchemaCalVer: calver, LoaderIdentity: identity}
	h := sha256.New()
	_, _ = h.Write([]byte(envHash))
	_, _ = h.Write([]byte{0}) // field separator (both halves are length-delimited; the marker keeps the framing explicit)
	_, _ = h.Write([]byte(calver))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(identity))
	return hex.EncodeToString(h.Sum(nil)), comps, nil
}

// sha256Hex is the hex SHA-256 of a byte slice (the content-addressed config-state digest).
func sha256Hex(b []byte) string {
	h := sha256.New()
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// loaderIdentity names the COMPILED loader logic in the cache key (RCA 2026-09-06: the
// Phase 3 from: name:tag split landed in loaderkit WITHOUT a schema-CalVer bump, so the key
// (envelope + schema CalVer) did not change and the cache served the PRE-SPLIT tree for up
// to the same-key/same-tree assumption — the exact gap the RCA measured). The identity is the
// sdk module version from the build info: loaderkit's parse/fold logic lives in the sdk module,
// so a loader-logic change necessarily ships in a NEW sdk version → a new identity → a new
// cache key. A new binary never inherits an older logic's tree. Fallback (no build info,
// e.g. go test): the caller's envelope already varies per project; the identity stays "bare"
// so tests in one binary share one namespace, exactly as before this component.
// loaderIdentityFn is a package var (not a const) so tests inject a DIFFERENT identity and
// prove both the key and the read-time drift detection respond to it.
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

// sdkModulePath is loaderkit's own module path — the identity key is the module that OWNS
// this file's logic (any rename here must rename this constant with it).
const sdkModulePath = "github.com/opencharly/sdk"

// materializedCacheEntry is one cache file's contents: when the materialization was resolved, the
// MarshalMaterialized bytes of the merged tree (the SAME wire envelope the loader-materialize leg
// returns, PluginKinds preserved — never a plain json.Marshal), and the components it was
// materialized under (the read's drift-detection contract — see materializeCacheComponents).
type materializedCacheEntry struct {
	Resolved   time.Time                  `json:"resolved"`
	Tree       []byte                     `json:"tree"`
	Components materializeCacheComponents `json:"components"`
}

// readMaterializedTree returns the cached tree for key when present, components-matched (AND
// materialized under the CURRENT components — the self-detection contract: a config, schema, or
// loader-logic change (any component drift) is a MISS, so the caller re-materializes and the
// write path UPDATES the entry automatically. Every failure — absent file, corrupt JSON, empty
// tree, stale TTL, component drift, disabled cache — is a miss; callers re-materialize.
func readMaterializedTree(key string, want materializeCacheComponents) ([]byte, bool) {
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
	if e.Components != want {
		// Detected: the config/schema/loader changed since this entry was written.
		return nil, false
	}
	// NO TIME VALIDITY: an entry whose components match is VALID regardless of age (the
	// Docker cache rule — valid while the inputs are the same). Reclamation of old entries is
	// the prune's job (write-time, storage-bound), never the read's.
	return e.Tree, true
}

// writeMaterializedTree persists a tree under key, atomically (tmp + rename so a concurrent
// lock-free reader never sees a torn entry) and best-effort (a write failure is silent — the
// cache is an optimization). The entry's Resolved is the write timestamp — RECLAMATION data
// only (the prune's ordering): validity is purely component-based, never time-based.
func writeMaterializedTree(key string, tree []byte, comps materializeCacheComponents) {
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
	e := materializedCacheEntry{Resolved: time.Now(), Tree: tree, Components: comps}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	final := filepath.Join(dir, key+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		return
	}
	// Docker-style opportunistic reclamation: keep the newest materializedCacheMaxEntries
	// entries, drop the rest — storage-bound ONLY (never a validity input; see the prune
	// doc). Best-effort like every cache write.
	pruneMaterializedCache(dir)
}

// pruneMaterializedCache reclaims storage Docker-style (the builder --keep-storage analogue):
// when the entry count exceeds materializedCacheMaxEntries, the OLDEST entries are removed
// (by write time). This is RECLAMATION — it never invalidates a valid entry: the read's
// validity is purely component-based (any charly.yml/schema/loader change re-materializes and
// writes a new entry automatically), so a pruned entry is only ever a stale-input orphan.
// The bound is a package var so tests can shrink it.
func pruneMaterializedCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	excess := len(entries) - materializedCacheMaxEntries
	if excess <= 0 {
		return
	}
	// os.ReadDir sorts by NAME (the key hash) — the reclamation must remove the OLDEST by
	// WRITE time, so sort by the entry's Resolved timestamp (the semantic write-time; the
	// file ModTime can drift with renames) ascending — the oldest first, removed first.
	type named struct {
		name string
		res  time.Time
	}
	byAge := make([]named, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		var ent materializedCacheEntry
		if json.Unmarshal(data, &ent) != nil {
			continue
		}
		byAge = append(byAge, named{e.Name(), ent.Resolved})
	}
	sort.Slice(byAge, func(i, j int) bool { return byAge[i].res.Before(byAge[j].res) })
	for _, e := range byAge {
		if excess <= 0 {
			return
		}
		_ = os.Remove(filepath.Join(dir, e.name))
		excess--
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
	key, comps, kerr := loadedProjectCacheKey(lp)
	if kerr != nil {
		return materialize(lp, merged, byID)
	}
	if tree, ok := readMaterializedTree(key, comps); ok {
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
	if tree, ok := readMaterializedTree(key, comps); ok {
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
		writeMaterializedTree(key, tree, comps)
	}
	return nil
}
