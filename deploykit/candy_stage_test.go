package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestCandyCopySource was relocated here from charly/refs_test.go (K3,
// Bucket-1 dissolution): the thin charly-core wrapper (Generator.candyCopySource)
// it exercised had zero non-test callers and was deleted, but the real logic —
// CandyCopySource's local-vs-remote build-context path resolution — lives in
// this package and stays real production code (used by EmitScratchStages,
// GenerateInitFragments's stage_fragment_copy rendering, and the pod-overlay
// build path).
func TestCandyCopySource(t *testing.T) {
	g := &Generator{
		Candies: map[string]CandyModel{
			"pixi": NewSpecCandyModel(spec.CandyModel{Name: "pixi"}, spec.CandyView{Remote: false}),
			"github.com/test/repo/layers/cuda": NewSpecCandyModel(
				spec.CandyModel{Name: "cuda", Version: "2026.167.1200"},
				spec.CandyView{Remote: true, RepoPath: "github.com/test/repo"},
			),
			"github.com/test/repo/layers/nover": NewSpecCandyModel(
				spec.CandyModel{Name: "nover"},
				spec.CandyView{Remote: true, RepoPath: "github.com/test/repo"},
			),
		},
	}

	if got := g.CandyCopySource("pixi"); got != "candy/pixi" {
		t.Errorf("local candy: got %q, want %q", got, "candy/pixi")
	}
	// Remote candies stage under the VERSION-keyed .build/_candy/<name>.<version>/
	// so distinct candy versions never share a dir (cross-version crossover).
	if got := g.CandyCopySource("github.com/test/repo/layers/cuda"); got != ".build/_candy/cuda.2026.167.1200" {
		t.Errorf("remote candy: got %q, want %q", got, ".build/_candy/cuda.2026.167.1200")
	}
	// Defensive: a version-less remote candy (should never happen — the resolver
	// hard-errors) falls back to the bare name rather than a trailing-dot dir.
	if got := g.CandyCopySource("github.com/test/repo/layers/nover"); got != ".build/_candy/nover" {
		t.Errorf("version-less remote candy: got %q, want %q", got, ".build/_candy/nover")
	}
}
