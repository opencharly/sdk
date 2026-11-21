package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// TestEmitBuilderStages_DeclaredBuilderThatCannotRunIsAHardError is the guard for
// opencharly/sdk#608: a candy that declares `aur:` packages while the aur builder is not
// available used to be skipped SILENTLY — the build exited 0 and the image shipped without
// the packages (measured on opencharly/layer-kde-shell: no /usr/bin/kdotool, green build).
// A declared builder that cannot run must fail the build, naming the candy.
func TestEmitBuilderStages_DeclaredBuilderThatCannotRunIsAHardError(t *testing.T) {
	// The rule is the DEFAULT for every builder format, not an aur special case: any candy
	// that declares packages for a builder which cannot run fails the build.
	for _, format := range []string{"aur", "pixi", "npm"} {
		candy := NewSpecCandyModel(
			spec.CandyModel{Name: "carrier", FormatSections: map[string]PackageSection{
				format: {Packages: []string{"some-package"}},
			}},
			spec.CandyView{Name: "carrier"},
		)
		g := &Generator{Candies: map[string]CandyModel{"carrier": candy}}
		img := &buildkit.ResolvedBox{BuilderConfig: &buildkit.BuilderConfig{
			Builder: map[string]*buildkit.BuilderDef{format: {}},
		}}
		var b strings.Builder
		err := g.EmitBuilderStages(&b, "carrier-box", img, []string{"carrier"})
		if err == nil {
			t.Fatalf("%s: a candy declaring %s packages with no %s builder must FAIL the build, got nil error", format, format, format)
		}
		if !strings.Contains(err.Error(), "carrier") || !strings.Contains(err.Error(), format) {
			t.Fatalf("%s: error %q must name the candy and the builder", format, err)
		}
	}
}

// A candy that declares nothing for the builder is untouched by the rule: no stage, no error.
func TestEmitBuilderStages_UndeclaredBuilderStaysSilent(t *testing.T) {
	candy := NewSpecCandyModel(spec.CandyModel{Name: "plain"}, spec.CandyView{Name: "plain"})
	g := &Generator{Candies: map[string]CandyModel{"plain": candy}}
	img := &buildkit.ResolvedBox{BuilderConfig: &buildkit.BuilderConfig{
		Builder: map[string]*buildkit.BuilderDef{"aur": {}},
	}}
	var b strings.Builder
	if err := g.EmitBuilderStages(&b, "plain-box", img, []string{"plain"}); err != nil {
		t.Fatalf("a candy declaring no aur packages must not fail: %v", err)
	}
}

// The widest suppression is "no builders configured at all": with a nil BuilderConfig the old
// code returned nil before looking at any candy, so a declared `aur:` package vanished with no
// error. The rule now runs FIRST, ahead of every early return.
func TestEmitBuilderStages_NoBuilderConfigAtAllStillRefusesDeclaredPackages(t *testing.T) {
	candy := NewSpecCandyModel(
		spec.CandyModel{Name: "kde-shell", FormatSections: map[string]PackageSection{
			"aur": {Packages: []string{"kdotool"}},
		}},
		spec.CandyView{Name: "kde-shell"},
	)
	g := &Generator{Candies: map[string]CandyModel{"kde-shell": candy}}
	img := &buildkit.ResolvedBox{} // BuilderConfig nil
	var b strings.Builder
	err := g.EmitBuilderStages(&b, "shell-box", img, []string{"kde-shell"})
	if err == nil || !strings.Contains(err.Error(), "no \"aur\" builder is configured") {
		t.Fatalf("nil BuilderConfig must refuse a declared aur package, got %v", err)
	}
}
