package loaderkit

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/internal/spectest"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/refs"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// spec.OpInContext is a package-level DI hook FinalizeScannedCandies → CompleteCandyRunOps calls for
// every scanned candy (spec/spec/injection_seams.go). In PRODUCTION charly core's own init() wires it
// (charly/charly/layers.go: spec.OpInContext = opInContext), safe there because charly always shares
// that process with the scan. This sdk-only test binary links no charly core, so the hook stays nil
// unless wired here — and the REAL layer-supervisord materialization the init-depends bed fetches
// carries a `run:` step, so the nil hook panics on the FIRST live run of a candy with one (the two
// pre-existing live beds' layer-direnv candies carry no run step, which is why they never reached it).
// The port itself lives ONCE, in sdk/internal/spectest — deploykit's compiler tests need the same
// classifier for their own sdk-only test binary, so it is one shared sdk-internal helper rather
// than a per-package copy (R3).
func init() {
	spectest.Install()
}

// scan_live_bed_test.go — the LIVE MECHANISM BED for the per-box scope fix (charly#735).
//
// Unlike scan_scope_test.go (which drives the real collector + fix-point + arbiter with a
// STUBBED fetch/scan), this bed runs the UNCHANGED production legs end to end against REAL
// materializations: the real reachability collector (CollectRemoteRefsOpts), the real repo-cache
// fetch (EnsureRepoDownloaded → the warmed git exports under ~/.cache/charly/repos), the real
// manifest scan (ScanRemoteCandy → ParseCandyManifest), and the real arbiter (PickCandyVersion).
// Nothing about the changed path is mocked — only the git network hop is served from the
// operator's warmed cache (an IMMUTABLE tag export never re-fetches).
//
// The fixture is the exact shape #735 §7/§9 diagnosed: ONE candy legitimately pinned at
// DIFFERENT real git tags by INDEPENDENT boxes. The chosen repo's two tags carry genuinely
// DIFFERING charly.yml content, so the content filter cannot mask the scope result. The bed
// asserts the authoritative rule:
//
//   - TWO INDEPENDENT BOXES at different tags  -> SILENT (no diagnostic at all);
//   - ONE BOX whose layers disagree            -> a genuine conflict -> INFO (never WARNING);
//   - the winner is the newest REFERENCED tag  -> never the newest remote.
//
// LIVE-OR-SKIP: the fixture needs the operator's warmed repo cache (~/.cache/charly/repos). Like
// every live-boundary test it SKIPS cleanly — visibly — when that cache is not present or the
// LIVE_SDK_MECHANISM_BED opt-in is unset, and never fakes the boundary. Run it for real with:
//
//	LIVE_SDK_MECHANISM_BED=1 go test ./loaderkit/ -run TestLiveBed -v
const (
	liveCandyRef  = "github.com/opencharly/layer-direnv"
	liveNewestTag = "v2026.272.0057" // newest REFERENCED — the winner
	liveOlderTag  = "v2026.242.1147" // an independent box's own pin (content DIFFERS)
)

// liveBedThreaded is a hand-built subset MODELLED ON the snapshot production supplies from charly's
// `loaderThreaded()` (charly/charly/loader_threaded.go) — the sdk layer cannot call it, so the bed
// carries only the DATA its own real materializations need. It is not the registry-derived snapshot
// itself, and an sdk test must not pretend to reproduce it; it holds the two facts the bed's manifests
// actually consult:
//   - Kinds: the bed's candy manifests stack a candy node with sibling skill/hook/marketplace
//     entities, so kind classification must recognize all four (an empty snapshot sends ParseDoc
//     down ParseCandyManifest's direct-mapping fallback, which never desugars verb sugar);
//   - Primaries: the SCALAR verb shorthand (`command: |`) desugars into the internal
//     plugin/plugin_input envelope only when the verb's primary field is known — `command`'s is
//     `command` (the map form `command: {command: …}` names it). This mirrors the `file` entry
//     candyThreaded declares for the same reason.
var liveBedThreaded = spec.Threaded{
	Kinds:     map[string]bool{"candy": true, "skill": true, "hook": true, "marketplace": true},
	Primaries: map[string]string{"command": "command"},
}

// liveBedParse is the REAL per-document parse seam (the same ParseCandyManifest candy/plugin-build
// wires), threading liveBedThreaded — the bed's hand-built, production-modelled snapshot — so the
// node-form branch desugars the authored verb sugar against it, exactly as production does.
//
// The bed's MigrateCache is a documented no-op, so the derived view still carries the pristine
// export's legacy top-level `version:` stamp. Production's command:migrate reshapes that stamp away
// BEFORE the parse, and ParseCandyManifest's node-form branch (the ONLY branch that desugars authored
// verb sugar) rejects a scalar top-level `version:` — it would fall back to a decoder that does not
// desugar, which the real layer-supervisord plan (`command: {command: supervisorctl pid,
// in_container: true}`) then fails. Dropping that ONE legacy directive here hands the parser the same
// node-form shape production hands it, without mutating the read-only view.
func liveBedParse(path string) (*spec.CandyYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if stripped, ok := stripLegacyTopLevelVersion(data); ok {
		tmp, terr := os.CreateTemp("", "livebed-*.yml")
		if terr != nil {
			return nil, terr
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if _, werr := tmp.Write(stripped); werr != nil {
			_ = tmp.Close()
			return nil, werr
		}
		if cerr := tmp.Close(); cerr != nil {
			return nil, cerr
		}
		path = tmp.Name()
	}
	return ParseCandyManifest(path, liveBedThreaded, spec.NewCandyVocab(nil))
}

// stripLegacyTopLevelVersion removes a legacy top-level `version:` directive from a candy-manifest
// byte stream and returns the rewritten bytes. It reports false when the stream carries no such
// directive (the already-migrated shape) or is not a single top-level mapping.
func stripLegacyTopLevelVersion(data []byte) ([]byte, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "version" {
			continue
		}
		root.Content = append(root.Content[:i], root.Content[i+2:]...)
		out, err := yaml.Marshal(&doc)
		if err != nil {
			return nil, false
		}
		return out, true
	}
	return nil, false
}

// liveBedSeams wires the REAL production legs. Only the network hop is served from the warmed
// cache (EnsureRepoDownloaded short-circuits immutable tags). MigrateCache is a documented no-op:
// the bed exercises the SCOPE arbitration, not cache migration.
func liveBedSeams(cfg *spec.Config, layers map[string]spec.CandyReader, levels *[]spec.DiagLevel, msgs *[]string) spec.ScanSeams {
	return spec.ScanSeams{
		CollectRemoteRefs: func(_ map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
			return CollectRemoteRefsOpts(cfg, layers, spec.ResolveOpts{}, spec.RefsCollectSeams{})
		},
		EnsureRepo: func(repoPath, version string) (string, error) {
			return EnsureRepoDownloaded(repoPath, version, spec.RefsCollectSeams{
				Downloader:   kit.DefaultDownloader{},
				MigrateCache: func(string) error { return nil },
			})
		},
		ScanRemote: func(cacheDir, repoPath string, wantRefs map[string]bool) (map[string]spec.ScannedCandy, error) {
			return ScanRemoteCandy(cacheDir, repoPath, wantRefs, liveBedParse)
		},
		Diag: func(level spec.DiagLevel, format string, args ...any) {
			*levels = append(*levels, level)
			*msgs = append(*msgs, fmt.Sprintf(format, args...))
		},
	}
}

// liveBedFixtureAvailable gates on the LIVE opt-in AND the warmed per-tag materializations the bed
// needs. Absent either -> the bed SKIPS visibly, never fakes the boundary.
func liveBedFixtureAvailable(t *testing.T) {
	if os.Getenv("LIVE_SDK_MECHANISM_BED") == "" {
		t.Skip("LIVE_SDK_MECHANISM_BED unset — skipping the live mechanism bed (set it to run against the warmed repo cache)")
	}
	for _, ver := range []string{liveNewestTag, liveOlderTag} {
		p, err := refs.RepoCachePath(liveCandyRef, ver)
		if err != nil {
			t.Fatalf("resolving cache path for %s@%s: %v", liveCandyRef, ver, err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Skipf("warmed repo cache missing %s@%s (%v) — skipping the live mechanism bed", liveCandyRef, ver, err)
		}
	}
}

func liveBedLayers() map[string]spec.CandyReader {
	return map[string]spec.CandyReader{
		"layer-older": newLoaderTestCandy("layer-older", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{spec.CandyRef("@" + liveCandyRef + ":" + liveOlderTag)},
		}),
		"layer-newest": newLoaderTestCandy("layer-newest", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{spec.CandyRef("@" + liveCandyRef + ":" + liveNewestTag)},
		}),
	}
}

func countLevels(levels []spec.DiagLevel, want spec.DiagLevel) int {
	n := 0
	for _, l := range levels {
		if l == want {
			n++
		}
	}
	return n
}

// TestLiveBedCrossBoxDifferenceIsSilent proves RULE §7 "two different boxes -> no notice at all"
// on the REAL path: two independent boxes, two REAL tags with differing content, NO diagnostic.
func TestLiveBedCrossBoxDifferenceIsSilent(t *testing.T) {
	liveBedFixtureAvailable(t)
	layers := liveBedLayers()
	cfg := &spec.Config{Box: spec.BoxMap{
		"box-older":  []byte(`{"candy": ["layer-older"]}`),
		"box-newest": []byte(`{"candy": ["layer-newest"]}`),
	}}
	var levels []spec.DiagLevel
	var msgs []string
	got, err := ScanCandyFromLocal(nil, nil, liveBedSeams(cfg, layers, &levels, &msgs))
	if err != nil {
		t.Fatalf("live scan failed: %v", err)
	}
	if len(levels) != 0 {
		t.Fatalf("two independent boxes at two real tags must be SILENT, got %v\n%s", levels, strings.Join(msgs, "\n"))
	}
	w, ok := got[liveCandyRef]
	if !ok {
		t.Fatalf("the ref must resolve; got keys %v", readerKeys(got))
	}
	if w.GetVersion() != liveNewestTag {
		t.Errorf("the newest REFERENCED tag must win, got %q (want %q)", w.GetVersion(), liveNewestTag)
	}
	t.Logf("LIVE cross-box: resolved %s@%s with ZERO diagnostics across 2 real tags", liveCandyRef, w.GetVersion())
}

// TestLiveBedSameBoxConflictIsInfo proves RULE §7 "one box, two layers disagree -> newest
// referenced + note (INFO)" on the REAL path: ONE box composing both layers -> a genuine same-box
// conflict reported, every diagnostic INFO (never WARNING), winner = newest referenced.
func TestLiveBedSameBoxConflictIsInfo(t *testing.T) {
	liveBedFixtureAvailable(t)
	layers := liveBedLayers()
	cfg := &spec.Config{Box: spec.BoxMap{
		"one-box": []byte(`{"candy": ["layer-older", "layer-newest"]}`),
	}}
	var levels []spec.DiagLevel
	var msgs []string
	got, err := ScanCandyFromLocal(nil, nil, liveBedSeams(cfg, layers, &levels, &msgs))
	if err != nil {
		t.Fatalf("live scan failed: %v", err)
	}
	if n := countLevels(levels, spec.DiagWarning); n != 0 {
		t.Fatalf("a resolvable same-box skew must NEVER be a WARNING, got %d warning(s):\n%s", n, strings.Join(msgs, "\n"))
	}
	if n := countLevels(levels, spec.DiagInfo); n < 1 {
		t.Fatalf("one box's disagreeing layers must report >=1 INFO, got %v", levels)
	}
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "layer-direnv") {
			found = true
		}
	}
	if !found {
		t.Errorf("the same-box conflict must name layer-direnv, got messages:\n%s", strings.Join(msgs, "\n"))
	}
	w, ok := got[liveCandyRef]
	if !ok {
		t.Fatalf("the ref must resolve; got keys %v", readerKeys(got))
	}
	if w.GetVersion() != liveNewestTag {
		t.Errorf("the newest referenced tag must still win, got %q (want %q)", w.GetVersion(), liveNewestTag)
	}
	t.Logf("LIVE same-box: resolved %s@%s; %d INFO, 0 WARNING:\n%s",
		liveCandyRef, w.GetVersion(), len(levels), strings.Join(msgs, "\n"))
}

// liveInitSeedFixtureAvailable gates the init-depends-seed bed on the LIVE opt-in AND the warmed
// init-runtime materialization the seed fetches. Absent either -> the bed SKIPS visibly, never
// fakes the boundary (the same live-or-skip contract as liveBedFixtureAvailable).
func liveInitSeedFixtureAvailable(t *testing.T) {
	t.Helper()
	if os.Getenv("LIVE_SDK_MECHANISM_BED") == "" {
		t.Skip("LIVE_SDK_MECHANISM_BED unset — skipping the live init-depends seed bed (set it to run against the warmed repo cache)")
	}
	p, err := refs.RepoCachePath(initRuntimeRepo, initRuntimeVer)
	if err != nil {
		t.Fatalf("resolving cache path for %s@%s: %v", initRuntimeRepo, initRuntimeVer, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("warmed repo cache missing %s@%s (%v) — skipping the live init-depends seed bed", initRuntimeRepo, initRuntimeVer, err)
	}
}

// TestLiveBedInitDependsSeedFetchesTheInitRuntime proves the NEW init-depends fetch seed end to end
// against a REAL materialization: a fully-local project whose only candy declares a non-packaged
// `exec:` service triggers supervisord, so ScanCandyFromLocal must FETCH supervisord's runtime candy
// (gitlink #336's seed) through the real repo-cache fetch + manifest scan and resolve it at the
// vocabulary's tag. The unit tests pin the seed's SHAPE against stubbed seams; this bed pins that the
// production legs materialize and parse the real `layer-supervisord` tree it names.
func TestLiveBedInitDependsSeedFetchesTheInitRuntime(t *testing.T) {
	liveInitSeedFixtureAvailable(t)
	localScanned := map[string]spec.ScannedCandy{"svc": scannedServiceCandy("svc", t.TempDir())}
	var levels []spec.DiagLevel
	var msgs []string
	seams := liveBedSeams(&spec.Config{}, map[string]spec.CandyReader{
		"svc": newLoaderTestCandy("svc", spec.CandyModel{}, spec.CandyView{}),
	}, &levels, &msgs)

	got, err := ScanCandyFromLocal(localScanned, supervisordInitCfg(), seams)
	if err != nil {
		t.Fatalf("live init-seed scan failed: %v\n%s", err, strings.Join(msgs, "\n"))
	}
	w, ok := got[initRuntimeRef]
	if !ok {
		t.Fatalf("the init-runtime candy %q must be fetched by the seed; got keys %v", initRuntimeRef, readerKeys(got))
	}
	if v := w.GetVersion(); v != initRuntimeVer {
		t.Errorf("init runtime resolved at %q, want the vocabulary's %q", v, initRuntimeVer)
	}
	t.Logf("LIVE init seed: materialized %s@%s with %d diagnostics:\n%s",
		initRuntimeRef, w.GetVersion(), len(levels), strings.Join(msgs, "\n"))
}

// TestLiveBedInjectionResolvesTheSeedsRealMaterialization closes the loop the seed bed above opens,
// and it is the LIVE PROOF ON THE COMMITTED TREE for this PR's two deploykit behaviour changes. It
// drives the REAL deploykit.InjectInitDependsCandy and the REAL
// deploykit.PruneContainerInitForSystemd — the committed functions, unmodified — over the SAME real
// materialization the seed produced, with the vocabulary's REAL ref-shaped `depends_candy`
// (supervisordInitCfg sets DependsCandy to initRuntimeRemote). What it reaches that the unit tests
// cannot is the actual KEYS a real scan registers a remote candy under — its full repo path AND its
// bare repo name — which is exactly the divergence the ref-shaped normalization and the tolerant
// prune exist to absorb.
//
// It FAILS without EITHER change:
//   - without the BareCandyRef normalization, `depends_candy` stays the raw `@github…:v…` ref, which
//     matches no scanned key and no order entry, so the pass reports the box unsatisfied and injects
//     nothing;
//   - without the tolerant prune, the ref-keyed entry is compared against the literal "supervisord"
//     and survives onto a systemd guest.
func TestLiveBedInjectionResolvesTheSeedsRealMaterialization(t *testing.T) {
	liveInitSeedFixtureAvailable(t)
	localScanned := map[string]spec.ScannedCandy{"svc": scannedServiceCandy("svc", t.TempDir())}
	var levels []spec.DiagLevel
	var msgs []string
	seams := liveBedSeams(&spec.Config{}, map[string]spec.CandyReader{
		"svc": newLoaderTestCandy("svc", spec.CandyModel{}, spec.CandyView{}),
	}, &levels, &msgs)

	scanned, err := ScanCandyFromLocal(localScanned, supervisordInitCfg(), seams)
	if err != nil {
		t.Fatalf("live init-seed scan failed: %v\n%s", err, strings.Join(msgs, "\n"))
	}
	if _, ok := scanned[initRuntimeRef]; !ok {
		t.Fatalf("the init-runtime candy %q must be materialized before the injection can see it; got keys %v",
			initRuntimeRef, readerKeys(scanned))
	}

	// The REAL injection pass, over the REAL scanned set, with the REAL ref-shaped depends_candy.
	cfg := &spec.Config{Box: spec.BoxMap{"svcbox": []byte(`{"candy": ["svc"]}`)}}
	deploykit.InjectInitDependsCandy(cfg, scanned, supervisordInitCfg())
	img, ok := cfg.BoxConfig("svcbox")
	if !ok {
		t.Fatal("box svcbox must survive the injection pass")
	}
	if len(img.Candy) == 0 || img.Candy[0] != initRuntimeRef {
		t.Fatalf("the ref-shaped depends_candy must inject the REAL scanned key %q, got %v",
			initRuntimeRef, img.Candy)
	}

	// The REAL machine-venue prune, over the order the injection just produced.
	pruned := deploykit.PruneContainerInitForSystemd(img.Candy, deploykit.HostContext{MachineVenue: true})
	if len(pruned) != 1 || pruned[0] != "svc" {
		t.Fatalf("the REAL ref-keyed init candy must be pruned on a machine venue, got %v", pruned)
	}
	t.Logf("LIVE init injection: scanned set carries %s (name %q); injected order %v; machine-venue prune -> %v",
		initRuntimeRef, scanned[initRuntimeRef].GetName(), img.Candy, pruned)
}
