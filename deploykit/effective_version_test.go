package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// effective_version_test.go (relocated from charly/effective_version_test.go's
// TestComputeEffectiveVersions, #55 K3 Cone 4): proves the image-version derivation that
// feeds the content-stable ai.opencharly.version label. With the schema-versioning removal
// cutover the authored dedicated `version:` source is GONE, so the derivation is: the
// highest source candy git tag across the candy chain; else the internal base's effective
// version (recurse); else EMPTY (no fabricated version — and no hard error either). Pure
// deploykit + literal spec fixtures, no charly loader machinery needed.

// newTestCandy wraps a CandyModel into a spec.CandyReader fixture, stamping name onto
// both views (mirrors charly's own candy_test_helpers_test.go:testCandy). Every current
// caller needs only the model side; the view is a bare named CandyView (unparam).
func newTestCandy(name string, m spec.CandyModel) spec.CandyReader {
	m.Name = name
	return NewSpecCandyModel(m, spec.CandyView{Name: name})
}

func TestComputeEffectiveVersions(t *testing.T) {
	layers := map[string]CandyModel{
		"a": newTestCandy("a", spec.CandyModel{Version: "2026.100.0000"}),
		"b": newTestCandy("b", spec.CandyModel{Version: "2026.200.0000"}), // newest candy
	}
	images := map[string]*ResolvedBox{
		// An image composing candies a+b derives the highest source candy version.
		"derived": {ResolvedBox: spec.ResolvedBox{Name: "derived", Candy: []string{"a", "b"}, IsExternalBase: true, Base: "quay.io/x:1"}},
		// A candy-free image on an INTERNAL base inherits the base's effective version
		// (reached through the candy chain the internal base carries).
		"passthrough": {ResolvedBox: spec.ResolvedBox{Name: "passthrough", Base: "derived"}},
		// A candy-free EXTERNAL-base image with nothing derivable is EMPTY — no fabricated
		// version and no hard error.
		"orphan": {ResolvedBox: spec.ResolvedBox{Name: "orphan", IsExternalBase: true, Base: "quay.io/x:1"}},
	}
	if err := ComputeEffectiveVersions(images, layers); err != nil {
		t.Fatalf("ComputeEffectiveVersions: %v", err)
	}

	cases := map[string]string{
		"derived":     "2026.200.0000", // highest source candy version
		"passthrough": "2026.200.0000", // inherited from the internal base
		"orphan":      "",              // nothing derivable — empty (no fabricated version)
	}
	for name, want := range cases {
		if got := images[name].EffectiveVersion; got != want {
			t.Errorf("%s: EffectiveVersion = %q, want %q", name, got, want)
		}
	}

	// A candy bump propagates to a deriving image's identity.
	layers["b"] = newTestCandy("b", spec.CandyModel{Version: "2026.400.0000"})
	derived := map[string]*ResolvedBox{"derived": {ResolvedBox: spec.ResolvedBox{Name: "derived", Candy: []string{"a", "b"}, IsExternalBase: true, Base: "quay.io/x:1"}}}
	if err := ComputeEffectiveVersions(derived, layers); err != nil {
		t.Fatal(err)
	}
	if got := derived["derived"].EffectiveVersion; got != "2026.400.0000" {
		t.Errorf("after candy bump: EffectiveVersion = %q, want 2026.400.0000", got)
	}
}
