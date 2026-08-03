package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// effective_version_test.go (relocated from charly/effective_version_test.go's
// TestComputeEffectiveVersions, #55 K3 Cone 4): proves the image-version derivation that
// feeds the content-stable ai.opencharly.version label — a dedicated version: wins;
// otherwise the highest candy version across the chain; a candyless image recurses to
// its internal base; a candyless external-base image with no version is a HARD ERROR
// (no build-timestamp fallback). Pure deploykit + literal spec fixtures, no charly
// loader machinery needed.

// newTestCandy wraps a CandyModel + CandyView into a spec.CandyReader fixture, stamping
// name onto both views (mirrors charly's own candy_test_helpers_test.go:testCandy).
func newTestCandy(name string, m spec.CandyModel, v spec.CandyView) spec.CandyReader {
	m.Name = name
	v.Name = name
	return NewSpecCandyModel(m, v)
}

func TestComputeEffectiveVersions(t *testing.T) {
	layers := map[string]CandyModel{
		"a": newTestCandy("a", spec.CandyModel{Version: "2026.100.0000"}, spec.CandyView{}),
		"b": newTestCandy("b", spec.CandyModel{Version: "2026.200.0000"}, spec.CandyView{}), // newest candy
	}
	images := map[string]*ResolvedBox{ // dedicated version wins over the (newer) candy versions.
		"dedicated":   {ResolvedBox: spec.ResolvedBox{Name: "dedicated", Version: "2026.050.0000", Candy: []string{"a", "b"}, IsExternalBase: true, Base: "quay.io/x:1"}}, // no dedicated version -> highest candy version (b = 2026.200.0000).
		"derived":     {ResolvedBox: spec.ResolvedBox{Name: "derived", Candy: []string{"a", "b"}, IsExternalBase: true, Base: "quay.io/x:1"}},                             // bare base: candyless + external + dedicated version (what `charly migrate` backfills).
		"barebase":    {ResolvedBox: spec.ResolvedBox{Name: "barebase", Version: "2026.300.0000", IsExternalBase: true, Base: "quay.io/x:1"}},                             // candyless on an INTERNAL base -> recurse to the base's effective version.
		"passthrough": {ResolvedBox: spec.ResolvedBox{Name: "passthrough", Base: "barebase"}}}
	if err := ComputeEffectiveVersions(images, layers); err != nil {
		t.Fatalf("ComputeEffectiveVersions: %v", err)
	}

	cases := map[string]string{
		"dedicated":   "2026.050.0000", // dedicated wins
		"derived":     "2026.200.0000", // highest candy version
		"barebase":    "2026.300.0000", // dedicated bare-base version
		"passthrough": "2026.300.0000", // recursed to barebase
	}
	for name, want := range cases {
		if got := images[name].EffectiveVersion; got != want {
			t.Errorf("%s: EffectiveVersion = %q, want %q", name, got, want)
		}
	}

	// A candy bump propagates to a deriving image's identity.
	layers["b"] = newTestCandy("b", spec.CandyModel{Version: "2026.400.0000"}, spec.CandyView{})
	derived := map[string]*ResolvedBox{"derived": {ResolvedBox: spec.ResolvedBox{Name: "derived", Candy: []string{"a", "b"}, IsExternalBase: true, Base: "quay.io/x:1"}}}
	if err := ComputeEffectiveVersions(derived, layers); err != nil {
		t.Fatal(err)
	}
	if got := derived["derived"].EffectiveVersion; got != "2026.400.0000" {
		t.Errorf("after candy bump: EffectiveVersion = %q, want 2026.400.0000", got)
	}

	// Hard error: candyless external-base image with no version (no fallback).
	orphan := map[string]*ResolvedBox{"orphan": {ResolvedBox: spec.ResolvedBox{Name: "orphan", IsExternalBase: true, Base: "quay.io/x:1"}}}
	if err := ComputeEffectiveVersions(orphan, map[string]CandyModel{}); err == nil {
		t.Error("expected a hard error for a candyless external-base image with no version:")
	}
}
