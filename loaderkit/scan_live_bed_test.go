package loaderkit

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/refs"
	"github.com/opencharly/spec/spec"
)

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

// liveBedParse is the REAL per-document parse seam (the same ParseCandyManifest candy/plugin-build
// wires), with an empty Threaded snapshot — sufficient for these node-form manifests, which route
// through ParseCandyManifest's direct mapping fallback.
func liveBedParse(path string) (*spec.CandyYAML, error) {
	return ParseCandyManifest(path, spec.Threaded{}, spec.NewCandyVocab(nil))
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
