package loaderkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// scan_scope_test.go — the SCOPE gate for the whole scan path, end to end.
//
// The assembled closure contains MANY independent boxes (charly's own + every imported
// `distro-*` namespace), and they legitimately pin the same candy at different tags. The
// pre-scoping arbiter keyed candidates by BARE REF ALONE, so those independent choices merged
// into one set and the closure emitted thousands of lines about a conflict no single
// composition ever saw. These tests drive the REAL fix-point (collect → fetch → scan →
// arbitrate) and hold the scope rule to its contract: cross-box difference silent, same-box
// conflict reported at INFO.

const scopeCandyRef = "github.com/opencharly/pod-dbus"

// scanSeamsForScope serves one materialization per requested download, keyed off the download's
// own RefReferrers so the test controls exactly which box named which tag.
func scanSeamsForScope(downloads []spec.RemoteDownload, levels *[]spec.DiagLevel) spec.ScanSeams {
	return spec.ScanSeams{
		CollectRemoteRefs: func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
			return downloads, nil
		},
		EnsureRepo: func(repoPath, version string) (string, error) {
			return "/cache/repos/" + repoPath + "@" + version, nil
		},
		ScanRemote: func(cacheDir, repoPath string, wantRefs map[string]bool) (map[string]spec.ScannedCandy, error) {
			out := make(map[string]spec.ScannedCandy, len(wantRefs))
			for ref := range wantRefs {
				// Content differs per version (a distinct TopPackages entry per tag), so the
				// content check cannot mask the scope result.
				out[ref] = spec.ScannedCandy{Model: spec.CandyModel{
					Name:        "dbus",
					SourceDir:   cacheDir,
					TopPackages: []string{"marker-" + versionOf(cacheDir)},
				}}
			}
			return out, nil
		},
		Diag: func(level spec.DiagLevel, format string, args ...any) {
			*levels = append(*levels, level)
		},
	}
}

func versionOf(cacheDir string) string {
	for i := len(cacheDir) - 1; i >= 0; i-- {
		if cacheDir[i] == '@' {
			return cacheDir[i+1:]
		}
	}
	return cacheDir
}

func downloadsForScope(tags []string, referrersByTag map[string][]string) []spec.RemoteDownload {
	out := make([]spec.RemoteDownload, 0, len(tags))
	for _, tag := range tags {
		out = append(out, spec.RemoteDownload{
			RepoPath:     "github.com/opencharly/pod-dbus",
			Version:      tag,
			Refs:         []string{scopeCandyRef},
			RefReferrers: map[string][]string{scopeCandyRef: referrersByTag[tag]},
		})
	}
	return out
}

// Two DIFFERENT boxes at two different tags: silent, and the newest REFERENCED tag still wins.
func TestScanScopeCrossBoxDifferenceIsSilent(t *testing.T) {
	downloads := downloadsForScope(
		[]string{"v2026.239.1555", "v2026.243.1831"},
		map[string][]string{
			"v2026.239.1555": {"box=fedora-coder"},
			"v2026.243.1831": {"box=versa"},
		},
	)
	var levels []spec.DiagLevel
	got, err := ScanCandyFromLocal(nil, nil, scanSeamsForScope(downloads, &levels))
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 0 {
		t.Fatalf("a cross-box version difference must be silent, got %v", levels)
	}
	w, ok := got[scopeCandyRef]
	if !ok {
		t.Fatalf("the ref must resolve; got keys %v", readerKeys(got))
	}
	if w.GetVersion() != "v2026.243.1831" {
		t.Errorf("the newest REFERENCED tag must win, got %q", w.GetVersion())
	}
}

// Two references INSIDE ONE box at two different tags: a genuine conflict — reported, at INFO.
func TestScanScopeSameBoxConflictIsInfo(t *testing.T) {
	downloads := downloadsForScope(
		[]string{"v2026.239.1555", "v2026.243.1831"},
		map[string][]string{
			"v2026.239.1555": {"box=one-box"},
			"v2026.243.1831": {"box=one-box"},
		},
	)
	var levels []spec.DiagLevel
	if _, err := ScanCandyFromLocal(nil, nil, scanSeamsForScope(downloads, &levels)); err != nil {
		t.Fatal(err)
	}
	if len(levels) != 1 || levels[0] != spec.DiagInfo {
		t.Fatalf("a same-box conflict must report exactly one INFO, got %v", levels)
	}
}

// A real multi-box closure: several boxes pin different tags, and ONE box's two references
// disagree. Exactly one INFO (that box's); every cross-box pair stays silent.
func TestScanScopeMixedClosureReportsOnlyTheSameBoxConflict(t *testing.T) {
	downloads := downloadsForScope(
		[]string{"v2026.235.2115", "v2026.239.1555", "v2026.243.1831"},
		map[string][]string{
			"v2026.235.2115": {"box=layer-consumer"},
			"v2026.239.1555": {"box=fedora-coder"},
			"v2026.243.1831": {"box=fedora-coder", "box=versa"},
		},
	)
	var levels []spec.DiagLevel
	if _, err := ScanCandyFromLocal(nil, nil, scanSeamsForScope(downloads, &levels)); err != nil {
		t.Fatal(err)
	}
	if len(levels) != 1 || levels[0] != spec.DiagInfo {
		t.Fatalf("exactly the fedora-coder same-box conflict must report one INFO, got %v", levels)
	}
}

// A scope whose OWN references disagree is reported even when the global winner comes from a
// DIFFERENT scope. The predecessor's winner-relative check compared every candidate only against
// the winner's referrers, so an intra-box conflict in a box that did not happen to hold the newest
// tag was silently MISSED. Conflicts are per-scope, independent of who won.
func TestPickCandyVersionConflictInNonWinnerScopeStillReports(t *testing.T) {
	d := &diagCollector{}
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.243.1831", Source: "hub@new", Referrers: []string{"box=a"}}, // global winner
		{GitTag: "v2026.235.2115", Source: "hub@old1", Referrers: []string{"box=b"}},
		{GitTag: "v2026.239.1555", Source: "hub@old2", Referrers: []string{"box=b"}},
	}
	best := PickCandyVersion("github.com/opencharly/pod-dbus", cands, d.sink())
	if best.GitTag != "v2026.243.1831" {
		t.Fatalf("global newest referenced must win, got %q", best.GitTag)
	}
	if len(d.levels) != 1 || d.levels[0] != spec.DiagInfo {
		t.Fatalf("box=b's intra-scope conflict must report one INFO, got %v", d.levels)
	}
	if !strings.Contains(d.msgs[0], "box=b") {
		t.Errorf("the diagnostic must name the conflicting scope, got %q", d.msgs[0])
	}
}

// Scope-conflict detection is per-scope: a scope with ONE tag never conflicts, no matter how many
// OTHER scopes reference that tag differently at the same time.
func TestPickCandyVersionSingleTagScopeNeverConflicts(t *testing.T) {
	d := &diagCollector{}
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.243.1831", Source: "x", Referrers: []string{"box=a"}},
		{GitTag: "v2026.235.2115", Source: "x", Referrers: []string{"box=b"}},
		{GitTag: "v2026.239.1555", Source: "x", Referrers: []string{"box=c"}},
	}
	PickCandyVersion("github.com/opencharly/thing", cands, d.sink())
	if len(d.levels) != 0 {
		t.Fatalf("three independent single-tag boxes must be silent, got %v", d.levels)
	}
}

// TestScanScopeEndToEndWiresRealCollector runs the REAL collector (CollectRemoteRefsOpts) into
// the REAL scan fix-point, so the collector's box-scope labels are proven to survive the wire
// (RemoteDownload.RefReferrers -> CandyCandidate.Referrers) and drive the arbitration. This is the
// integration the unit tests on each half cannot cover: two INDEPENDENT boxes pinning the same
// candy at two tags must stay SILENT end to end, while ONE box whose two layers disagree must
// report exactly one INFO.
func TestScanScopeEndToEndWiresRealCollector(t *testing.T) {
	const ref = "github.com/opencharly/pod-dbus"
	// Two boxes, two tags — an independent pin per box (the false-positive shape).
	layers := map[string]spec.CandyReader{
		"layer-a": newLoaderTestCandy("layer-a", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{"@github.com/opencharly/pod-dbus:v2026.239.1555"},
		}),
		"layer-b": newLoaderTestCandy("layer-b", spec.CandyModel{}, spec.CandyView{
			Require: []spec.CandyRef{"@github.com/opencharly/pod-dbus:v2026.243.1831"},
		}),
	}
	cfg := &spec.Config{Box: spec.BoxMap{
		"fedora-coder": json.RawMessage(`{"candy": ["layer-a"]}`),
		"versa":        json.RawMessage(`{"candy": ["layer-b"]}`),
	}}

	collect := func(_ map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
		return CollectRemoteRefsOpts(cfg, layers, spec.ResolveOpts{}, spec.RefsCollectSeams{})
	}
	var levels []spec.DiagLevel
	seams := spec.ScanSeams{
		CollectRemoteRefs: collect,
		EnsureRepo:        func(repoPath, version string) (string, error) { return "/cache/" + repoPath + "@" + version, nil },
		ScanRemote: func(cacheDir, repoPath string, wantRefs map[string]bool) (map[string]spec.ScannedCandy, error) {
			out := map[string]spec.ScannedCandy{}
			for r := range wantRefs {
				out[r] = spec.ScannedCandy{Model: spec.CandyModel{
					Name:        "dbus",
					SourceDir:   cacheDir,
					TopPackages: []string{"marker-" + versionOf(cacheDir)},
				}}
			}
			return out, nil
		},
		Diag: func(level spec.DiagLevel, format string, args ...any) { levels = append(levels, level) },
	}
	got, err := ScanCandyFromLocal(nil, nil, seams)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 0 {
		t.Fatalf("two independent boxes at two tags must be SILENT end to end, got %v", levels)
	}
	if w, ok := got[ref]; !ok || w.GetVersion() != "v2026.243.1831" {
		t.Fatalf("the newest REFERENCED tag must win, got %v", got[ref])
	}

	// Now put BOTH layers in ONE box — a genuine same-box conflict → exactly one INFO.
	cfg.Box = spec.BoxMap{"one-box": json.RawMessage(`{"candy": ["layer-a", "layer-b"]}`)}
	levels = nil
	got, err = ScanCandyFromLocal(nil, nil, seams)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 1 || levels[0] != spec.DiagInfo {
		t.Fatalf("two layers inside ONE box must report exactly one INFO, got %v", levels)
	}
	if w, ok := got[ref]; !ok || w.GetVersion() != "v2026.243.1831" {
		t.Fatalf("the newest referenced tag must still win, got %v", got[ref])
	}
}

func readerKeys(m map[string]spec.CandyReader) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
