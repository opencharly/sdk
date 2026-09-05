package loaderkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// load_cache_test.go — the materialized-tree cache contract (load_cache.go): a same-state
// materialization is computed ONCE and reused; the second load performs NO unify (the
// underlying materializer is called EXACTLY once — the counter is the cache-hit witness). The
// no-cache behavior (the materializer called once per load) fails the SameState assertion, so
// removing the cache wrapper from LoadSeamsFromExecutor breaks this test — see also
// TestMaterializedCache_DisabledReMaterializes for the explicit no-cache signature.

// countingExecutor implements spec.LoaderExecutor with a COUNTING materialize leg: the
// materialize-call count is the cache-hit witness (a hit serves the stored tree WITHOUT calling
// the leg). WalkProject returns the canned envelope regardless of the input dir, so a test can
// present the SAME project state to both loads (the walk itself is out of scope — the cache
// wraps only the materialize unify).
type countingExecutor struct {
	materializeCalls int
	canned           spec.LoadedProject
	result           spec.UnifiedFile
}

func (c *countingExecutor) LoaderThreaded() spec.Threaded { return spec.Threaded{} }

func (c *countingExecutor) RunBootstrapPhase(data []byte) ([]byte, error) { return data, nil }

func (c *countingExecutor) WalkProject(_ string, _ []byte) (spec.LoadedProject, error) {
	return c.canned, nil
}

func (c *countingExecutor) MaterializeLoadedProject(_ *spec.LoadedProject, merged *spec.UnifiedFile, _ map[int64]*spec.UnifiedFile) error {
	c.materializeCalls++
	*merged = c.result
	return nil
}

func (c *countingExecutor) ValidateAndroidDevices(*spec.UnifiedFile) error { return nil }

func (c *countingExecutor) ValidatePreemptible(*spec.UnifiedFile) error { return nil }

// testMaterializedState builds the canned project state (the walk envelope) + the canned
// materialize result (the merged tree) a counting executor serves. The result carries PluginKinds
// + Namespaces so the cache round-trip proves it preserves the out-of-band wire state a plain
// json.Marshal drops.
func testMaterializedState() (spec.LoadedProject, spec.UnifiedFile) {
	lp := spec.LoadedProject{
		ID: 7,
		Docs: []spec.LoadedDoc{{
			SrcLabel:   "test/charly.yml:doc0",
			SrcDir:     "/proj",
			Directives: []byte("version: " + kit.LatestSchemaVersion().String() + "\n"),
			Project: spec.ParsedProject{
				Version: kit.LatestSchemaVersion().String(),
				Nodes: []spec.ParsedNode{{
					Name: "app", Disc: "deploy",
					Body: json.RawMessage(`{"box":"fedora","vm":{"ram_gb":4}}`),
				}},
			},
		}},
	}
	res := spec.UnifiedFile{
		Version: kit.LatestSchemaVersion().String(),
		RootDir: "/proj",
		PluginKinds: map[string]map[string]json.RawMessage{
			"local": {"tpl": json.RawMessage(`{"from":"base"}`)},
		},
		Namespaces: map[string]*spec.UnifiedFile{
			"ns": {RootDir: "/proj/ns", Version: kit.LatestSchemaVersion().String()},
		},
	}
	return lp, res
}

// isolateCacheRoot points the materialized-tree cache at a fresh temp dir for the test's
// lifetime (never the developer's real ~/.cache/charly).
func isolateCacheRoot(t *testing.T) {
	t.Helper()
	t.Setenv(materializedCacheEnvName, t.TempDir())
}

// TestMaterializedCache_SameStateMaterializesOnce is THE cache contract: two sequential
// materializations on the SAME project state run the unify (the underlying materializer) EXACTLY
// once — the second load is served from the cache. The materializer call-count is the witness;
// without the cache the second call would materialize again and this test FAILS (count becomes 2).
func TestMaterializedCache_SameStateMaterializesOnce(t *testing.T) {
	isolateCacheRoot(t)
	lp, res := testMaterializedState()
	exec := &countingExecutor{canned: lp, result: res}
	seams := LoadSeamsFromExecutor(exec)

	merged1 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged1, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	if exec.materializeCalls != 1 {
		t.Fatalf("first load must materialize exactly once, got %d", exec.materializeCalls)
	}
	merged2 := &spec.UnifiedFile{}
	byID := map[int64]*spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged2, byID); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	if exec.materializeCalls != 1 {
		t.Fatalf("second load on the SAME state must perform NO unify (cache hit), got %d materializations", exec.materializeCalls)
	}
	if !reflect.DeepEqual(merged1, merged2) {
		t.Errorf("cached reconstruction differs from the materialized original:\n got  %#v\n want %#v", merged2, merged1)
	}
	// The hit reproduces MaterializeLoadedProject's byID registration (project id → merged tree).
	if byID[7] != merged2 {
		t.Errorf("cache hit did not register byID[7] = merged, got %v", byID[7])
	}
}

// connectPassCountingExecutor is a countingExecutor that ALSO reports the connect-pass state
// (implements the optional connectPassAwareExecutor capability) — the compiled-in host's shape.
// The connect-pass flag is settable so a test can flip it between loads.
type connectPassCountingExecutor struct {
	countingExecutor
	inConnectPass bool
}

func (c *connectPassCountingExecutor) InKindConnectPass() bool { return c.inConnectPass }

// TestMaterializedCache_ConnectPassBypassesCache is the R1 regression gate for the external-kind
// decode regression: a load running INSIDE the walk's connect-declared-kind pre-pass must NOT be
// cached (its materialization DEFERS the declared-but-unconnected kind nodes — caching it would
// hand the outer load a tree with the kind entities missing). The witness: with the connect-pass
// flag true, two same-state loads BOTH materialize (the cache is bypassed); with the flag false
// the same two loads materialize ONCE (the normal cache contract, TestMaterializedCache_
// SameStateMaterializesOnce). Removing the bypass from LoadSeamsFromExecutor breaks this test.
func TestMaterializedCache_ConnectPassBypassesCache(t *testing.T) {
	isolateCacheRoot(t)
	lp, res := testMaterializedState()

	// Connect pass active: every load materializes directly — the deferred-entity tree must
	// never be cached under the outer load's key.
	exec := &connectPassCountingExecutor{countingExecutor: countingExecutor{canned: lp, result: res}, inConnectPass: true}
	seams := LoadSeamsFromExecutor(exec)
	for i := 0; i < 2; i++ {
		merged := &spec.UnifiedFile{}
		if err := seams.MaterializeLoadedProject(&lp, merged, map[int64]*spec.UnifiedFile{}); err != nil {
			t.Fatalf("connect-pass load %d: %v", i+1, err)
		}
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("connect pass must bypass the cache (materialize per load), got %d calls", exec.materializeCalls)
	}

	// Connect pass inactive: the normal cache contract holds (one unify for same-state loads).
	exec2 := &connectPassCountingExecutor{countingExecutor: countingExecutor{canned: lp, result: res}, inConnectPass: false}
	seams2 := LoadSeamsFromExecutor(exec2)
	for i := 0; i < 2; i++ {
		merged := &spec.UnifiedFile{}
		if err := seams2.MaterializeLoadedProject(&lp, merged, map[int64]*spec.UnifiedFile{}); err != nil {
			t.Fatalf("normal load %d: %v", i+1, err)
		}
	}
	if exec2.materializeCalls != 1 {
		t.Fatalf("normal load must cache (one unify), got %d calls", exec2.materializeCalls)
	}
}

// TestMaterializedCache_DisabledReMaterializes is the explicit no-cache signature: with the cache
// disabled every load materializes — exactly the failure mode "the materializer call-count > 1"
// the SameState test would hit if the cache were removed.
func TestMaterializedCache_DisabledReMaterializes(t *testing.T) {
	isolateCacheRoot(t)
	prev := materializedTreeCacheEnabled
	materializedTreeCacheEnabled = false
	defer func() { materializedTreeCacheEnabled = prev }()

	lp, res := testMaterializedState()
	exec := &countingExecutor{canned: lp, result: res}
	seams := LoadSeamsFromExecutor(exec)
	for i := 0; i < 2; i++ {
		merged := &spec.UnifiedFile{}
		if err := seams.MaterializeLoadedProject(&lp, merged, map[int64]*spec.UnifiedFile{}); err != nil {
			t.Fatalf("load %d: %v", i+1, err)
		}
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("disabled cache must materialize per load, got %d calls", exec.materializeCalls)
	}
}

// TestMaterializedCache_TTLExpiryReMaterializes proves the TTL: an entry past materializeCacheTTL
// is a miss and the unify re-runs (a negative TTL is always stale — deterministic, no sleep).
func TestMaterializedCache_TTLExpiryReMaterializes(t *testing.T) {
	isolateCacheRoot(t)
	prev := materializeCacheTTL
	materializeCacheTTL = -time.Nanosecond
	defer func() { materializeCacheTTL = prev }()

	lp, res := testMaterializedState()
	exec := &countingExecutor{canned: lp, result: res}
	seams := LoadSeamsFromExecutor(exec)
	for i := 0; i < 2; i++ {
		merged := &spec.UnifiedFile{}
		if err := seams.MaterializeLoadedProject(&lp, merged, map[int64]*spec.UnifiedFile{}); err != nil {
			t.Fatalf("load %d: %v", i+1, err)
		}
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("expired entry must re-materialize, got %d calls", exec.materializeCalls)
	}
}

// TestMaterializedCache_KeySensitiveToEnvelope is the invalidation rule: any config/ref change =
// a new key. Two envelopes differing in ANY state byte (a config body here) must materialize
// twice; the derived keys must differ.
func TestMaterializedCache_KeySensitiveToEnvelope(t *testing.T) {
	isolateCacheRoot(t)
	lp1, res := testMaterializedState()
	// Deep-clone via a JSON round-trip (a struct copy shares Docs' backing array, so a mutation
	// through lp2 would hit lp1 too — the test must present two GENUINELY different states).
	lp1JSON, err := json.Marshal(lp1)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	var lp2 spec.LoadedProject
	if err := json.Unmarshal(lp1JSON, &lp2); err != nil {
		t.Fatalf("clone: %v", err)
	}
	lp2.Docs[0].Project.Nodes[0].Body = json.RawMessage(`{"box":"fedora","vm":{"ram_gb":8}}`)

	k1, err := loadedProjectCacheKey(&lp1)
	if err != nil {
		t.Fatalf("key 1: %v", err)
	}
	k2, err := loadedProjectCacheKey(&lp2)
	if err != nil {
		t.Fatalf("key 2: %v", err)
	}
	if k1 == k2 {
		t.Fatalf("different project states produced the same cache key %q — the invalidation rule (any config/ref change = a new key) is broken", k1)
	}

	exec := &countingExecutor{canned: lp1, result: res}
	seams := LoadSeamsFromExecutor(exec)
	for _, lp := range []*spec.LoadedProject{&lp1, &lp2} {
		merged := &spec.UnifiedFile{}
		if err := seams.MaterializeLoadedProject(lp, merged, map[int64]*spec.UnifiedFile{}); err != nil {
			t.Fatalf("load: %v", err)
		}
	}
	if exec.materializeCalls != 2 {
		t.Fatalf("changed state must re-materialize, got %d calls", exec.materializeCalls)
	}
}

// TestMaterializedCache_CorruptEntryDegradesAndSelfHeals proves the failure contract: a corrupt
// cache entry is a miss (never a load error), the unify re-runs, and the STORE overwrites the bad
// file — so the NEXT same-state load hits a good entry.
func TestMaterializedCache_CorruptEntryDegradesAndSelfHeals(t *testing.T) {
	isolateCacheRoot(t)
	lp, res := testMaterializedState()
	key, err := loadedProjectCacheKey(&lp)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	cacheDir, err := materializedCacheDir()
	if err != nil {
		t.Fatalf("cache dir: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, key+".json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt entry: %v", err)
	}

	exec := &countingExecutor{canned: lp, result: res}
	seams := LoadSeamsFromExecutor(exec)
	merged1 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged1, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("load over corrupt entry must not fail: %v", err)
	}
	if exec.materializeCalls != 1 {
		t.Fatalf("corrupt entry must re-materialize once, got %d calls", exec.materializeCalls)
	}
	merged2 := &spec.UnifiedFile{}
	if err := seams.MaterializeLoadedProject(&lp, merged2, map[int64]*spec.UnifiedFile{}); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if exec.materializeCalls != 1 {
		t.Fatalf("self-healed entry must serve the second load, got %d calls", exec.materializeCalls)
	}
	if !reflect.DeepEqual(merged1, merged2) {
		t.Errorf("self-healed reconstruction differs from the original")
	}
}

// TestMaterializedCache_LoadUnifiedEndToEnd drives the FULL LoadUnified orchestration twice over
// the same project state: bootstrap + walk (stubbed) + materialize + the whole validation chain,
// on BOTH loads — the second load runs every step EXCEPT the materialize unify. Proves the cache
// is not just seam-deep: a cached tree passes GateSchemaVersion + all validators identically.
func TestMaterializedCache_LoadUnifiedEndToEnd(t *testing.T) {
	isolateCacheRoot(t)
	dir := t.TempDir()
	latest := kit.LatestSchemaVersion().String()
	if err := os.WriteFile(filepath.Join(dir, spec.UnifiedFileName), []byte("version: "+latest+"\n"), 0o644); err != nil {
		t.Fatalf("write root config: %v", err)
	}
	lp, res := testMaterializedState()
	exec := &countingExecutor{canned: lp, result: res}
	seams := LoadSeamsFromExecutor(exec)

	first, ok, err := LoadUnified(dir, seams)
	if err != nil || !ok || first == nil {
		t.Fatalf("first LoadUnified: ok=%v err=%v", ok, err)
	}
	second, ok, err := LoadUnified(dir, seams)
	if err != nil || !ok || second == nil {
		t.Fatalf("second LoadUnified: ok=%v err=%v", ok, err)
	}
	if exec.materializeCalls != 1 {
		t.Fatalf("two LoadUnified calls on the same state must materialize ONCE, got %d", exec.materializeCalls)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second load's cached tree differs from the first:\n got  %#v\n want %#v", second, first)
	}
}
