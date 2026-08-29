
package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// TestGenerateInitFragments_NilLayerInOrder is the regression guard for the
// nil-layer panic: a deploy's candyOrder can include an add_candy that the
// build's layers map does not carry (e.g. check-agentteams-snapshot's
// layer-agentteams-snapshot add_candy during prepare-venue). GenerateInitFragments
// must skip a candy absent from g.Candies instead of dereferencing a nil layer.
func TestGenerateInitFragments_NilLayerInOrder(t *testing.T) {
	tmpDir := t.TempDir()
	g := &Generator{
		BuildDir: tmpDir,
		Boxes: map[string]*buildkit.ResolvedBox{
			"app": {ResolvedBox: spec.ResolvedBox{Name: "app", Distro: []string{"fedora"}}},
		},
		Candies: map[string]CandyModel{
			"svc": NewSpecCandyModel(spec.CandyModel{
				Name:    "svc",
				Service: []spec.ServiceEntry{{Name: "svc", Exec: "svc serve"}},
			}, spec.CandyView{}),
		},
		RenderService: fakeRenderService,
	}
	def := &spec.ResolvedInit{
		Model:       "fragment_assembly",
		FragmentDir: "supervisor",
		ServiceSchema: &spec.InitServiceSchema{
			SupportsPackaged: false,
			ServiceTemplate:  "[program:{{.Name}}]\ncommand={{.Exec}}\n",
		},
	}
	// "missing" is NOT in g.Candies — the pre-fix code panics on layer.Service().
	if err := g.GenerateInitFragments("app", "supervisord", def, []string{"missing", "svc"}); err != nil {
		t.Fatalf("GenerateInitFragments with a nil layer: %v", err)
	}
}
