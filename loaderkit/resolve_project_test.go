package loaderkit

import (
	"slices"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// resolve_project_test.go — the ENVELOPE-ASSEMBLER seam of the init `depends_candy:` injection.
//
// deploykit/init_depends_test.go pins what InjectInitDependsCandy DECIDES (both init directions,
// the remote-key case, idempotence, the no-op boundaries). Nothing there pins that
// ProjectResolvedProject actually CALLS it — and that seam is not the build path: it feeds the
// validate / inspect / deploy-deploy envelope, whose consumers (candy/plugin-fleet's
// resolveBoxSelection) read rp.Boxes[...] to decide what a deploy installs. A silent no-op here is
// therefore a real deploy defect that the build-path coverage cannot see.
//
// This test seals both halves of the wiring, and fails if either regresses: the call itself, and
// the SECOND-pass projection ordering it forced (the injection rewrites img.Candy while
// ProjectResolvedBox copies that list into the view, so every fresh box must be resolved before
// any is projected — a single-pass loop would project the pre-injection list).
//
// The fixtures are deliberately minimal rather than a copy of deploykit's init-vocabulary fixture:
// the init RESOLUTION semantics are pinned there, so all this seam needs is one composition whose
// active init declares a depends_candy.

// TestProjectResolvedProject_InjectsInitDependsCandy is the container direction through the whole
// fresh-box arm: a box composing a service candy — and NOT naming an init — must have the
// supervisord candy present on its PROJECTED view, not merely on the intermediate resolved box.
func TestProjectResolvedProject_InjectsInitDependsCandy(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"tutorial-shell": spec.EncodeBox(spec.BoxConfig{
				Base:  "quay.io/fedora/fedora:43",
				Build: []string{"rpm"},
				Candy: []string{"sshd"},
			}),
		},
	}
	layers := map[string]spec.CandyReader{
		// A service candy triggers supervisord (the InitSystems map is what scan-time
		// PopulateCandyInitSystem fills and CandyReader.HasInit reads) but installs nothing.
		"sshd": deploykit.NewSpecCandyModel(
			spec.CandyModel{Name: "sshd"},
			spec.CandyView{Name: "sshd", InitSystems: map[string]bool{"supervisord": true}},
		),
		// The init's own candy — present in the project's scanned set, absent from the box.
		"supervisord": deploykit.NewSpecCandyModel(
			spec.CandyModel{Name: "supervisord"},
			spec.CandyView{Name: "supervisord"},
		),
	}
	initCfg := &buildkit.InitConfig{
		Init: map[string]*spec.ResolvedInit{
			"supervisord": {
				CandyFields:   []string{"service"},
				DependsCandy:  "supervisord",
				ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "[program:{{.Name}}]"},
			},
		},
	}

	dir := t.TempDir()
	seams := ResolveProjectSeams{
		// The real resolver, exactly as the host closure wraps it — so the test exercises the
		// genuine resolve → inject → project chain rather than a hand-built box.
		ResolveBox: func(c *spec.Config, name, calver, d string) (*buildkit.ResolvedBox, error) {
			return buildkit.ResolveBox(c, name, calver, d, buildkit.ResolveOpts{
				DistroCfg:  &buildkit.DistroConfig{},
				BuilderCfg: &buildkit.BuilderConfig{},
			})
		},
		FillNamespacedBoxes: func(*spec.UnifiedFile, *buildkit.InitConfig, string, string, string, *spec.ResolvedProject, map[*spec.UnifiedFile]bool) {
		},
		ResolveResources:      func(*spec.UnifiedFile) map[string]*spec.ResolvedResource { return nil },
		ShouldIncludeDisabled: func(string) bool { return false },
		ComputeIntermediates: func(boxes map[string]*buildkit.ResolvedBox, _ map[string]spec.CandyReader, _ *spec.Config, _ string) (map[string]*buildkit.ResolvedBox, error) {
			return boxes, nil
		},
	}

	rp, err := ProjectResolvedProject(cfg, layers, nil, &buildkit.DistroConfig{}, &buildkit.BuilderConfig{}, initCfg, dir, "2026.1.1", "2026.1.1", seams, nil, nil)
	if err != nil {
		t.Fatalf("ProjectResolvedProject: %v", err)
	}

	view, ok := rp.Boxes["tutorial-shell"]
	if !ok {
		t.Fatalf("resolved box view missing; got %v", rp.Boxes)
	}
	if !slices.Contains(view.Candy, "supervisord") {
		t.Fatalf("projected view must carry the injected init candy, got %v", view.Candy)
	}
	if view.Candy[0] != "supervisord" {
		t.Errorf("injected candy must lead the projected list, got %v", view.Candy)
	}
}

// TestProjectResolvedProject_FoldsNamespacedBedsIntoDeploy pins the envelope's deploy tree to the
// SAME namespace-folded set uf.Beds() walks. A superproject whose beds live in an IMPORTED
// namespace (the umbrella root importing `charly/`) has an empty uf.Deploy at the root, so before
// this fix rp.Deploy was empty and `charly check live <ns.bed>` from the umbrella root could not
// classify the bed's venue — it fell through to a container lookup
// (`container charly-charly.check-charly-vm is not running`). This test fails without the fold.
func TestProjectResolvedProject_FoldsNamespacedBedsIntoDeploy(t *testing.T) {
	disposable := true
	uf := &spec.UnifiedFile{
		Namespaces: map[string]*spec.UnifiedFile{
			"charly": {
				Deploy: map[string]spec.DeployNode{
					"check-x-vm": {Target: "vm", Disposable: &disposable},
				},
			},
		},
	}
	if len(uf.Deploy) != 0 {
		t.Fatal("test assumption broken: root uf.Deploy must be empty (the fold is what populates rp.Deploy)")
	}
	dir := t.TempDir()
	seams := ResolveProjectSeams{
		ResolveBox: func(c *spec.Config, name, calver, d string) (*buildkit.ResolvedBox, error) {
			return buildkit.ResolveBox(c, name, calver, d, buildkit.ResolveOpts{DistroCfg: &buildkit.DistroConfig{}, BuilderCfg: &buildkit.BuilderConfig{}})
		},
		FillNamespacedBoxes: func(*spec.UnifiedFile, *buildkit.InitConfig, string, string, string, *spec.ResolvedProject, map[*spec.UnifiedFile]bool) {
		},
		ResolveResources:      func(*spec.UnifiedFile) map[string]*spec.ResolvedResource { return nil },
		ShouldIncludeDisabled: func(string) bool { return false },
		ComputeIntermediates: func(boxes map[string]*buildkit.ResolvedBox, _ map[string]spec.CandyReader, _ *spec.Config, _ string) (map[string]*buildkit.ResolvedBox, error) {
			return boxes, nil
		},
	}
	rp, err := ProjectResolvedProject(&spec.Config{}, map[string]spec.CandyReader{}, uf, &buildkit.DistroConfig{}, &buildkit.BuilderConfig{}, &buildkit.InitConfig{}, dir, "2026.1.1", "2026.1.1", seams, nil, nil)
	if err != nil {
		t.Fatalf("ProjectResolvedProject: %v", err)
	}
	if _, ok := rp.Deploy["charly.check-x-vm"]; !ok {
		keys := make([]string, 0, len(rp.Deploy))
		for k := range rp.Deploy {
			keys = append(keys, k)
		}
		t.Fatalf("namespaced bed absent from rp.Deploy; got keys %v", keys)
	}
}
