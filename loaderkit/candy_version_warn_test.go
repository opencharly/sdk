package loaderkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// candsWith returns candidates at the given tags, each carrying the same referrer scope set.
func candsWith(tags []string, referrers ...string) []spec.CandyCandidate {
	out := make([]spec.CandyCandidate, 0, len(tags))
	for _, t := range tags {
		out = append(out, spec.CandyCandidate{
			GitTag:    t,
			Source:    "hub@" + t,
			Referrers: append([]string(nil), referrers...),
		})
	}
	return out
}

// diagCollector records every diagnostic with its level, so a test can assert BOTH that a
// line fired and at which severity.
type diagCollector struct {
	levels []spec.DiagLevel
	msgs   []string
}

func (d *diagCollector) sink() func(spec.DiagLevel, string, ...any) {
	return func(level spec.DiagLevel, format string, args ...any) {
		d.levels = append(d.levels, level)
		d.msgs = append(d.msgs, fmt.Sprintf(format, args...))
	}
}

// The advisory must reach an injected sink as DATA, carrying its LEVEL. Before this it was a
// bare stderr write, so `charly box validate` could not count warnings; before the severity
// fix it could only count them as WARNINGS, which gated a resolvable closure.
func TestPickCandyVersionRoutesAdvisoryToSinkAtInfo(t *testing.T) {
	d := &diagCollector{}
	best := PickCandyVersion("acme/thing", candsWith([]string{"v2026.237.557", "v2026.242.1648"}, "box=b1"), d.sink())
	if best.GitTag != "v2026.242.1648" {
		t.Errorf("arbiter picked %q, want the newest referenced source git tag", best.GitTag)
	}
	if len(d.levels) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %d", len(d.levels))
	}
	if d.levels[0] != spec.DiagInfo {
		t.Errorf("a resolvable skew must be INFO, got %q", d.levels[0])
	}
	if !strings.Contains(d.msgs[0], "resolved to multiple git tags") {
		t.Errorf("advisory text changed: %q", d.msgs[0])
	}
}

// THE SCOPE RULE, and the whole point of the change: two DIFFERENT boxes pinning different
// versions of the same candy is NOT a conflict. Each box is its own immutable composition and
// neither ever saw the other's version, so there is nothing to report.
func TestPickCandyVersionCrossBoxDifferenceIsSilent(t *testing.T) {
	d := &diagCollector{}
	// Two candidates at different tags named by DIFFERENT boxes → no shared scope.
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.235.2115", Source: "hub@old", Referrers: []string{"box=fedora-coder"}},
		{GitTag: "v2026.243.1831", Source: "hub@new", Referrers: []string{"box=arch-coder"}},
	}
	best := PickCandyVersion("github.com/opencharly/pod-dbus", cands, d.sink())
	if len(d.levels) != 0 {
		t.Fatalf("a cross-box version difference must be silent, got %d diagnostic(s): %q", len(d.levels), d.msgs)
	}
	if best.GitTag != "v2026.243.1831" {
		t.Errorf("the newest REFERENCED tag must still win, got %q", best.GitTag)
	}
}

// A genuine SAME-scope conflict (two references inside ONE box) still reports — at INFO, since
// the newest-referenced winner resolves it.
func TestPickCandyVersionSameBoxConflictIsInfo(t *testing.T) {
	d := &diagCollector{}
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.235.2115", Source: "hub@old", Referrers: []string{"box=one-box"}},
		{GitTag: "v2026.243.1831", Source: "hub@new", Referrers: []string{"box=one-box"}},
	}
	PickCandyVersion("github.com/opencharly/pod-dbus", cands, d.sink())
	if len(d.levels) != 1 || d.levels[0] != spec.DiagInfo {
		t.Fatalf("a same-box conflict must report exactly one INFO, got %v", d.levels)
	}
}

// A shared LAYER is NOT conflict-eligible. The version rule is scoped to a BOX ("multiple
// layers inside the same box"); a layer's own require: list is an independent composition with
// no box to conflict within, so a cross-tag difference attributed only to its own layer scope
// stays SILENT (R2/#739 — no reclassification of an independent composition into a conflict).
func TestPickCandyVersionSharedLayerIsNotAConflict(t *testing.T) {
	d := &diagCollector{}
	cands := []spec.CandyCandidate{
		{GitTag: "v2026.235.2115", Source: "hub@old", Referrers: []string{"layer=layer-x"}},
		{GitTag: "v2026.243.1831", Source: "hub@new", Referrers: []string{"layer=layer-x", "box=unrelated"}},
	}
	best := PickCandyVersion("github.com/opencharly/thing", cands, d.sink())
	if len(d.levels) != 0 {
		t.Fatalf("a layer-only scope is not a box and must be silent, got %d: %q", len(d.levels), d.msgs)
	}
	if best.GitTag != "v2026.243.1831" {
		t.Errorf("the newest REFERENCED tag must still win, got %q", best.GitTag)
	}
}

// No skew must produce NO diagnostic — otherwise a counted total would overstate.
func TestPickCandyVersionSilentWhenVersionsAgree(t *testing.T) {
	d := &diagCollector{}
	PickCandyVersion("acme/thing", candsWith([]string{"v2026.242.1648", "v2026.242.1648"}, "box=b1"), d.sink())
	if len(d.levels) != 0 {
		t.Errorf("identical git tags must not report, got %v", d.levels)
	}
}

// An UNRESOLVABLE set — no candidates at all — is the ONLY warning tier.
func TestPickCandyVersionNoCandidatesWarns(t *testing.T) {
	d := &diagCollector{}
	best := PickCandyVersion("acme/thing", nil, d.sink())
	if len(d.levels) != 1 || d.levels[0] != spec.DiagWarning {
		t.Fatalf("an empty candidate set must be the one WARNING, got %v", d.levels)
	}
	if best.GitTag != "" {
		t.Errorf("no candidate means no winner, got %q", best.GitTag)
	}
}

// nil selects stderr EXPLICITLY. There is no two-argument shim to fall back on: every caller
// states where its diagnostics go.
func TestPickCandyVersionNilSinkStillArbitrates(t *testing.T) {
	best := PickCandyVersion("acme/thing", candsWith([]string{"v2026.237.557", "v2026.242.1648"}, "box=b1"), nil)
	if best.GitTag != "v2026.242.1648" {
		t.Errorf("legacy form picked %q, want the newest", best.GitTag)
	}
}

// materialize writes a candy's charly.yml into a fresh per-tag directory and returns a
// candidate whose Scanned.Model.SourceDir points at it — the shape the scan produces for a
// remote materialization (one dir per (repo, git-tag)).
func materialize(t *testing.T, tag, body string, referrers ...string) spec.CandyCandidate {
	t.Helper()
	dir := filepath.Join(t.TempDir(), tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, spec.UnifiedFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return spec.CandyCandidate{
		Scanned:   spec.ScannedCandy{Model: spec.CandyModel{Name: "dev-tools", SourceDir: dir}},
		GitTag:    tag,
		Source:    "github.com/opencharly/layer-dev-tools@" + tag,
		Referrers: referrers,
	}
}

// A RE-TAG OF BYTE-IDENTICAL CONTENT MUST NOT REPORT. This is the regression the content
// check exists for: `layer-dev-tools v2026.235.2056 ≡ v2026.239.1624` (a sibling landing in
// the same hub repo re-mints the tag, the candy's bytes are unchanged) produced ~1356 lines
// across the assembled bed that named a difference which does not exist.
func TestPickCandyVersionIdenticalRetagIsSilent(t *testing.T) {
	body := "dev-tools:\n  candy:\n    description: unchanged\n    package: [ripgrep, htop]\n"
	cands := []spec.CandyCandidate{
		materialize(t, "v2026.235.2056", body, "box=shared"),
		materialize(t, "v2026.239.1624", body, "box=shared"),
	}
	d := &diagCollector{}
	best := PickCandyVersion("github.com/opencharly/layer-dev-tools", cands, d.sink())
	if len(d.levels) != 0 {
		t.Errorf("a byte-identical re-tag must be silent, got %v", d.msgs)
	}
	if best.GitTag != "v2026.239.1624" {
		t.Errorf("newest tag must still win on a silent re-tag, got %q", best.GitTag)
	}
}

// A GENUINELY-DIFFERING SAME-SCOPE PAIR MUST STILL REPORT. The change narrows WHEN the
// diagnostic fires; it must not suppress the one case a reader can act on — two tags whose
// candy content differs within one composition.
func TestPickCandyVersionDifferingContentSameScopeStillReports(t *testing.T) {
	oldBody := "dev-tools:\n  candy:\n    description: old\n    package: [ripgrep]\n"
	newBody := "dev-tools:\n  candy:\n    description: new\n    package: [ripgrep, htop, bat]\n"
	cands := []spec.CandyCandidate{
		materialize(t, "v2026.235.2056", oldBody, "box=shared"),
		materialize(t, "v2026.239.1624", newBody, "box=shared"),
	}
	d := &diagCollector{}
	best := PickCandyVersion("github.com/opencharly/layer-dev-tools", cands, d.sink())
	if len(d.levels) != 1 {
		t.Fatalf("differing same-scope content must report exactly once, got %d", len(d.levels))
	}
	if d.levels[0] != spec.DiagInfo {
		t.Errorf("a resolvable same-scope difference is INFO, got %q", d.levels[0])
	}
	if !strings.Contains(d.msgs[0], "differing content") {
		t.Errorf("diagnostic must name the content difference, got %q", d.msgs[0])
	}
	if best.GitTag != "v2026.239.1624" {
		t.Errorf("newest tag must win, got %q", best.GitTag)
	}
}

// When a manifest cannot be read but the scanned bodies are present and identical, the
// canonical-JSON fallback still proves a silent re-tag — and, crucially, it does NOT confuse
// two scans whose only difference is the per-tag cache path (SourceDir).
func TestPickCandyVersionIdenticalScanWithoutManifestIsSilent(t *testing.T) {
	mk := func(tag string) spec.CandyCandidate {
		return spec.CandyCandidate{
			Scanned: spec.ScannedCandy{Model: spec.CandyModel{
				Name:        "x",
				SourceDir:   "/cache/github.com/o/r@" + tag, // differs by tag; must NOT count as content
				HasContent:  true,
				TopPackages: []string{"ripgrep", "htop"},
			}},
			GitTag:    tag,
			Source:    "o/r@" + tag,
			Referrers: []string{"box=shared"},
		}
	}
	d := &diagCollector{}
	PickCandyVersion("github.com/o/r", []spec.CandyCandidate{mk("v2026.235.2056"), mk("v2026.239.1624")}, d.sink())
	if len(d.levels) != 0 {
		t.Errorf("identical scanned bodies must be silent, got %v", d.levels)
	}
}

// NO CONTENT SIGNAL MUST REPORT — the conservative direction. A candidate carrying neither a
// readable manifest nor a non-zero scan cannot be proven identical, and absence of proof is
// not proof of sameness, so the same-scope pair still reports (never a silent suppression).
func TestPickCandyVersionNoContentSignalStillReports(t *testing.T) {
	d := &diagCollector{}
	PickCandyVersion("acme/thing", candsWith([]string{"v2026.237.557", "v2026.242.1648"}, "box=b1"), d.sink())
	if len(d.levels) != 1 {
		t.Errorf("unprovable same-scope identity must report once, got %v", d.levels)
	}
}
