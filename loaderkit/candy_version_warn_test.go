package loaderkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func skewCands() []spec.CandyCandidate {
	return []spec.CandyCandidate{
		{GitTag: "v2026.237.557", Source: "old@v2026.237.557"},
		{GitTag: "v2026.242.1648", Source: "new@v2026.242.1648"},
	}
}

// The advisory must reach an injected sink as DATA. Before this it was a bare stderr write,
// so `charly box validate` could not count warnings and its summary could only omit the number
// or state a false one.
func TestPickCandyVersionWithRoutesAdvisoryToSink(t *testing.T) {
	var got []string
	best := PickCandyVersion("acme/thing", skewCands(), func(f string, a ...any) {
		got = append(got, f)
	})
	if best.GitTag != "v2026.242.1648" {
		t.Errorf("arbiter picked %q, want the newest source git tag", best.GitTag)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one advisory, got %d", len(got))
	}
	if !strings.Contains(got[0], "resolved to multiple git tags") {
		t.Errorf("advisory text changed: %q", got[0])
	}
}

// No skew must produce NO advisory — otherwise a counted total would overstate.
func TestPickCandyVersionWithSilentWhenVersionsAgree(t *testing.T) {
	same := []spec.CandyCandidate{
		{GitTag: "v2026.242.1648", Source: "a"},
		{GitTag: "v2026.242.1648", Source: "b"},
	}
	n := 0
	PickCandyVersion("acme/thing", same, func(string, ...any) { n++ })
	if n != 0 {
		t.Errorf("identical git tags must not warn, got %d advisories", n)
	}
}

// nil selects stderr EXPLICITLY. There is no two-argument shim to fall back on: every caller
// states where its advisories go.
func TestPickCandyVersionNilSinkStillArbitrates(t *testing.T) {
	best := PickCandyVersion("acme/thing", skewCands(), nil)
	if best.GitTag != "v2026.242.1648" {
		t.Errorf("legacy form picked %q, want the newest", best.GitTag)
	}
}

// materialize writes a candy's charly.yml into a fresh per-tag directory and returns a
// candidate whose Scanned.Model.SourceDir points at it — the shape the scan produces for a
// remote materialization (one dir per (repo, git-tag)).
func materialize(t *testing.T, tag, body string) spec.CandyCandidate {
	t.Helper()
	dir := filepath.Join(t.TempDir(), tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, spec.UnifiedFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return spec.CandyCandidate{
		Scanned: spec.ScannedCandy{Model: spec.CandyModel{Name: "dev-tools", SourceDir: dir}},
		GitTag:  tag,
		Source:  "github.com/opencharly/layer-dev-tools@" + tag,
	}
}

// A RE-TAG OF BYTE-IDENTICAL CONTENT MUST NOT WARN. This is the regression the whole change
// exists for: `layer-dev-tools v2026.235.2056 ≡ v2026.239.1624` (a sibling landing in the
// same hub repo re-mints the tag, the candy's bytes are unchanged) produced ~1356 advisory
// lines across the assembled bed that named a difference which does not exist. The winner
// is still the newest tag; for identical bytes the choice is moot, so no line is warranted.
func TestPickCandyVersionIdenticalRetagIsSilent(t *testing.T) {
	body := "dev-tools:\n  candy:\n    description: unchanged\n    package: [ripgrep, htop]\n"
	cands := []spec.CandyCandidate{
		materialize(t, "v2026.235.2056", body),
		materialize(t, "v2026.239.1624", body),
	}
	n := 0
	best := PickCandyVersion("github.com/opencharly/layer-dev-tools", cands, func(string, ...any) { n++ })
	if n != 0 {
		t.Errorf("a byte-identical re-tag must not warn, got %d advisories", n)
	}
	if best.GitTag != "v2026.239.1624" {
		t.Errorf("newest tag must still win on a silent re-tag, got %q", best.GitTag)
	}
}

// A GENUINELY-DIFFERING PAIR MUST STILL WARN. The change narrows WHEN the advisory fires; it
// must not suppress the one case a reader can act on — two tags whose candy content differs.
func TestPickCandyVersionDifferingContentStillWarns(t *testing.T) {
	oldBody := "dev-tools:\n  candy:\n    description: old\n    package: [ripgrep]\n"
	newBody := "dev-tools:\n  candy:\n    description: new\n    package: [ripgrep, htop, bat]\n"
	cands := []spec.CandyCandidate{
		materialize(t, "v2026.235.2056", oldBody),
		materialize(t, "v2026.239.1624", newBody),
	}
	var got []string
	best := PickCandyVersion("github.com/opencharly/layer-dev-tools", cands, func(f string, a ...any) {
		got = append(got, f)
	})
	if len(got) != 1 {
		t.Fatalf("differing content must warn exactly once, got %d", len(got))
	}
	if !strings.Contains(got[0], "differing content") {
		t.Errorf("advisory must name the content difference, got %q", got[0])
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
			GitTag: tag,
			Source: "o/r@" + tag,
		}
	}
	n := 0
	PickCandyVersion("github.com/o/r", []spec.CandyCandidate{mk("v2026.235.2056"), mk("v2026.239.1624")},
		func(string, ...any) { n++ })
	if n != 0 {
		t.Errorf("identical scanned bodies must not warn, got %d advisories", n)
	}
}

// NO CONTENT SIGNAL MUST WARN — the conservative direction. A candidate carrying neither a
// readable manifest nor a non-zero scan cannot be proven identical, and absence of proof is
// not proof of sameness, so the pair still warns (never a silent suppression).
func TestPickCandyVersionNoContentSignalStillWarns(t *testing.T) {
	n := 0
	PickCandyVersion("acme/thing", skewCands(), func(string, ...any) { n++ })
	if n != 1 {
		t.Errorf("unprovable identity must warn once, got %d", n)
	}
}
