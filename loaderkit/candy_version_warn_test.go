package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func skewCands() []spec.CandyCandidate {
	return []spec.CandyCandidate{
		{Version: "2026.144.1443", GitTag: "v2026.237.557", Source: "old@v2026.237.557"},
		{Version: "2026.242.1655", GitTag: "v2026.242.1648", Source: "new@v2026.242.1648"},
	}
}

// The advisory must reach an injected sink as DATA. Before this it was a bare stderr write,
// so `charly box validate` could not count warnings and its summary could only omit the number
// or state a false one.
func TestPickCandyVersionWithRoutesAdvisoryToSink(t *testing.T) {
	var got []string
	best := PickCandyVersionWith("acme/thing", skewCands(), func(f string, a ...any) {
		got = append(got, f)
	})
	if best.Version != "2026.242.1655" {
		t.Errorf("arbiter picked %q, want the newest per-entity version", best.Version)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one advisory, got %d", len(got))
	}
	if !strings.Contains(got[0], "resolved to multiple versions") {
		t.Errorf("advisory text changed: %q", got[0])
	}
}

// No skew must produce NO advisory — otherwise a counted total would overstate.
func TestPickCandyVersionWithSilentWhenVersionsAgree(t *testing.T) {
	same := []spec.CandyCandidate{
		{Version: "2026.242.1655", GitTag: "v2026.242.1648", Source: "a"},
		{Version: "2026.242.1655", GitTag: "v2026.242.1700", Source: "b"},
	}
	n := 0
	PickCandyVersionWith("acme/thing", same, func(string, ...any) { n++ })
	if n != 0 {
		t.Errorf("identical versions must not warn, got %d advisories", n)
	}
}

// The two-argument form still exists and still works, so existing callers and their tests
// compile and behave unchanged — this seam is additive, not a breaking change.
func TestPickCandyVersionKeepsLegacyShape(t *testing.T) {
	best := PickCandyVersion("acme/thing", skewCands())
	if best.Version != "2026.242.1655" {
		t.Errorf("legacy form picked %q, want the newest", best.Version)
	}
}
