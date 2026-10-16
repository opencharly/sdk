package loaderkit

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// skillNode builds one authored `skill:` node body — only `owner` matters to the projection.
func skillNode(owner string) json.RawMessage {
	b, err := json.Marshal(map[string]any{
		"owner":       owner,
		"family":      "internals",
		"description": "d",
		"content":     "body",
	})
	if err != nil {
		panic(err)
	}
	return b
}

// inertSeams is the minimal seam set ProjectResolvedProject needs to assemble a one-box project
// without touching a host: no namespace fill, no resources, the real box resolver.
func inertSeams() ResolveProjectSeams {
	return ResolveProjectSeams{
		ResolveBox: func(c *spec.Config, name, calver, d string) (*buildkit.ResolvedBox, error) {
			return buildkit.ResolveBox(c, name, calver, d, buildkit.ResolveOpts{
				DistroCfg:  &buildkit.DistroConfig{},
				BuilderCfg: &buildkit.BuilderConfig{},
			})
		},
		FillNamespacedBoxes: func(*spec.UnifiedFile, *buildkit.InitConfig, string, string, string, *spec.ResolvedProject, NamespaceAncestors) {
		},
		ResolveResources:      func(*spec.UnifiedFile) map[string]*spec.ResolvedResource { return nil },
		ShouldIncludeDisabled: func(string) bool { return false },
		ComputeIntermediates: func(boxes map[string]*buildkit.ResolvedBox, _ map[string]spec.CandyReader, _ *spec.Config, _ string) (map[string]*buildkit.ResolvedBox, error) {
			return boxes, nil
		},
	}
}

// TestProjectResolvedProject_FillsOwnedSkillsOntoTheWireModel is the coverage for the behaviour this
// change exists for (opencharly/spec#254's consumer step): the wire CandyModel must carry the skills
// its candy OWNS, so a consumer holding only the model can answer "which skills does this candy
// carry?" without reaching cfg.Skills.
//
// It exercises the FILL, not the helper: ProjectResolvedProject is the path the PR changes, and the
// helper's own unit tests cannot show that the fill is wired to it. The second arm is the "fails
// without the change" proof and needs no separate mutation — `uf` is what carries the authored
// skill nodes, so passing nil is exactly the pre-change behaviour, and Skills must then stay absent.
func TestProjectResolvedProject_FillsOwnedSkillsOntoTheWireModel(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"skills-box": spec.EncodeBox(spec.BoxConfig{
				Base:  "quay.io/fedora/fedora:43",
				Build: []string{"rpm"},
				Candy: []string{"charly-internals"},
			}),
		},
	}
	layers := map[string]spec.CandyReader{
		"charly-internals": deploykit.NewSpecCandyModel(
			spec.CandyModel{Name: "charly-internals"},
			spec.CandyView{Name: "charly-internals"},
		),
	}

	uf := &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{
			"skill": {
				"go":           skillNode("charly-internals"),
				"git-workflow": skillNode("charly-internals"),
				"local-deploy": skillNode("charly-local"), // owned by a candy outside this project
			},
		},
	}

	dir := t.TempDir()
	initCfg := &buildkit.InitConfig{}
	seams := inertSeams()

	rp, err := ProjectResolvedProject(cfg, layers, uf, &buildkit.DistroConfig{}, &buildkit.BuilderConfig{}, initCfg, dir, "2026.1.1", "2026.1.1", seams, nil, nil)
	if err != nil {
		t.Fatalf("ProjectResolvedProject: %v", err)
	}

	m, ok := rp.CandyModels["charly-internals"]
	if !ok {
		t.Fatal("charly-internals missing from rp.CandyModels")
	}
	if want := []string{"git-workflow", "go"}; !reflect.DeepEqual(m.Skills, want) {
		t.Fatalf("rp.CandyModels[charly-internals].Skills = %v, want %v — the wire model must carry the "+
			"skills its candy OWNS, and only those (local-deploy is owned by charly-local)", m.Skills, want)
	}

	// The pre-change behaviour, and therefore the "fails without the change" proof: with no authored
	// document there is nothing to project from, and the field must stay ABSENT rather than empty.
	rpNil, err := ProjectResolvedProject(cfg, layers, nil, &buildkit.DistroConfig{}, &buildkit.BuilderConfig{}, initCfg, dir, "2026.1.1", "2026.1.1", seams, nil, nil)
	if err != nil {
		t.Fatalf("ProjectResolvedProject(nil uf): %v", err)
	}
	if got := rpNil.CandyModels["charly-internals"].Skills; got != nil {
		t.Fatalf("with no authored document, Skills = %v, want nil (absent, not an empty list)", got)
	}
}
