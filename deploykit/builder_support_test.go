package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
	"github.com/opencharly/sdk/vmshared"
)

// builder_support_test.go — migrated from charly/generate_test.go (P8b render-glue
// remainder cutover): the charly-core `Generator.collectBuilderRuntimeEnv` was a dead
// 1-line pass-through (`g.toDeploykit().CollectBuilderRuntimeEnv`) with zero production
// callers — every real caller already calls deploykit.Generator.CollectBuilderRuntimeEnv
// directly (candy_env.go, render_prep.go). The shim + its tests are deleted from charly;
// this file carries the regression coverage forward against the real implementation,
// using this package's own pixiCandy/testCandy fixtures (intermediates_move_test.go).

// TestCollectBuilderRuntimeEnv_TriggeredEmitsRuntimeEnv is the regression for the
// 2026-04-29 jupyter-PATH-bug cutover. The pixi builder's runtime env contract
// (PIXI_CACHE_DIR + RATTLER_CACHE_DIR + ~/.pixi/{bin,envs/default/bin}) must reach any
// image whose candies have `pixi.toml` — even if `pixi` is NOT a top-level candy.
func TestCollectBuilderRuntimeEnv_TriggeredEmitsRuntimeEnv(t *testing.T) {
	g := &Generator{
		Candies: map[string]CandyModel{
			"jupyter": pixiCandy(t, spec.CandyModel{}, spec.CandyView{Name: "jupyter"}),
		},
	}
	img := &buildkit.ResolvedBox{
		Home: "/home/user",
		BuilderConfig: &buildkit.BuilderConfig{
			Builder: map[string]*vmshared.BuilderDef{
				"pixi": {
					DetectFiles:       []string{"pixi.toml", "pyproject.toml"},
					RuntimeEnv:        map[string]string{"PIXI_CACHE_DIR": "~/.cache/pixi"},
					PathContributions: []string{"~/.pixi/bin", "~/.pixi/envs/default/bin"},
				},
			},
		},
	}

	got := g.CollectBuilderRuntimeEnv([]string{"jupyter"}, img)
	if len(got) != 1 {
		t.Fatalf("got %d EnvConfigs, want 1", len(got))
	}
	cfg := got[0]
	if cfg.Vars["PIXI_CACHE_DIR"] != "~/.cache/pixi" {
		t.Errorf("Vars[PIXI_CACHE_DIR] = %q, want \"~/.cache/pixi\"", cfg.Vars["PIXI_CACHE_DIR"])
	}
	if len(cfg.PathAppend) != 2 || cfg.PathAppend[0] != "~/.pixi/bin" || cfg.PathAppend[1] != "~/.pixi/envs/default/bin" {
		t.Errorf("PathAppend = %v, want [~/.pixi/bin ~/.pixi/envs/default/bin]", cfg.PathAppend)
	}
}

// TestCollectBuilderRuntimeEnv_NotTriggered: when no candy triggers a builder, the
// builder must NOT contribute. Otherwise every image would inherit pixi env even when
// it has no Python in it.
func TestCollectBuilderRuntimeEnv_NotTriggered(t *testing.T) {
	g := &Generator{
		Candies: map[string]CandyModel{
			"chrome": candyFixture(spec.CandyModel{}, spec.CandyView{Name: "chrome"}), // no pixi.toml, no pyproject.toml
		},
	}
	img := &buildkit.ResolvedBox{
		Home: "/home/user",
		BuilderConfig: &buildkit.BuilderConfig{
			Builder: map[string]*vmshared.BuilderDef{
				"pixi": {
					DetectFiles:       []string{"pixi.toml"},
					RuntimeEnv:        map[string]string{"PIXI_CACHE_DIR": "~/.cache/pixi"},
					PathContributions: []string{"~/.pixi/envs/default/bin"},
				},
			},
		},
	}

	got := g.CollectBuilderRuntimeEnv([]string{"chrome"}, img)
	if got != nil {
		t.Errorf("expected no contributions when no layer triggers builder, got %v", got)
	}
}

// TestCollectBuilderRuntimeEnv_MultipleCandies verifies that even when many candies
// trigger the same builder (a future Python-heavy image where every candy has its own
// pixi.toml), the builder is counted once — no duplicate ENV PATH entries.
func TestCollectBuilderRuntimeEnv_MultipleCandies(t *testing.T) {
	g := &Generator{
		Candies: map[string]CandyModel{
			"a": pixiCandy(t, spec.CandyModel{}, spec.CandyView{Name: "a"}),
			"b": pixiCandy(t, spec.CandyModel{}, spec.CandyView{Name: "b"}),
			"c": pixiCandy(t, spec.CandyModel{}, spec.CandyView{Name: "c"}),
		},
	}
	img := &buildkit.ResolvedBox{
		Home: "/home/user",
		BuilderConfig: &buildkit.BuilderConfig{
			Builder: map[string]*vmshared.BuilderDef{
				"pixi": {
					DetectFiles:       []string{"pixi.toml"},
					PathContributions: []string{"~/.pixi/bin"},
				},
			},
		},
	}
	got := g.CollectBuilderRuntimeEnv([]string{"a", "b", "c"}, img)
	if len(got) != 1 {
		t.Errorf("got %d EnvConfigs, want 1 (de-duped)", len(got))
	}
}

// TestCollectBuilderRuntimeEnv_NilBuilderConfig: defensive — a project without a
// build.yml/`inits:` block leaves BuilderConfig nil. Don't panic.
func TestCollectBuilderRuntimeEnv_NilBuilderConfig(t *testing.T) {
	g := &Generator{Candies: map[string]CandyModel{"x": pixiCandy(t, spec.CandyModel{}, spec.CandyView{Name: "x"})}}
	img := &buildkit.ResolvedBox{Home: "/home/user", BuilderConfig: nil}
	got := g.CollectBuilderRuntimeEnv([]string{"x"}, img)
	if got != nil {
		t.Errorf("expected nil when BuilderConfig is nil, got %v", got)
	}
}
