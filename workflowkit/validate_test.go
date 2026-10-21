package workflowkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

func TestValidatePipelineAcceptsAValidPipeline(t *testing.T) {
	if err := ValidatePipeline(e2ePipeline()); err != nil {
		t.Fatalf("ValidatePipeline rejected a valid pipeline: %v", err)
	}
	if err := ValidatePipeline(nil); err == nil {
		t.Error("ValidatePipeline accepted nil")
	}
}

// TestValidatePipelineAcceptsForEach is an EXACT-EMPTINESS check: a valid for_each step
// must validate with ZERO errors. A substring assertion is not enough here — a stray
// "execution arms set" alongside the expected text passes a Contains check while the
// pipeline is still rejected, which is exactly how a steps:/for_each double-count hides.
// `steps:` is the for_each BODY (spec groups it under "for_each companions"), never an
// arm of its own.
func TestValidatePipelineAcceptsForEach(t *testing.T) {
	cases := []struct {
		name string
		p    *spec.Pipeline
	}{
		{
			name: "bare for_each",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "a", Run: "echo x"},
				{Id: "fan", ForEach: "$a.json.items",
					Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}},
			}},
		},
		{
			name: "for_each with every companion",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "a", Run: "echo x"},
				{Id: "fan", ForEach: "$a.json.items", ItemVar: "item", IndexVar: "i",
					BatchSize: 2, PauseMs: 100,
					Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}},
			}},
		},
		{
			name: "for_each with a sub-step arm of its own",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "a", Run: "echo x"},
				{Id: "fan", ForEach: "$a.json.items", Steps: []spec.PipelineSubStep{
					{Id: "s1", Run: "true"},
					{Id: "s2", Charly: []string{"status"}},
				}},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePipeline(tc.p); err != nil {
				t.Fatalf("ValidatePipeline rejected a valid %s pipeline: %v", tc.name, err)
			}
		})
	}
}

// TestValidatePipelineDecodesAuthoredLanesStep pins the AUTHORED boundary, which is the
// boundary the issue measured: the YAML a `kind: pipeline` author writes must decode onto
// the arm through the spec-pinned `lanes:` key AND pass the gate. A test that only builds
// Go structs would pass while the authored key was dropped on decode.
func TestValidatePipelineDecodesAuthoredLanesStep(t *testing.T) {
	const authored = `
description: pooled fan-out
steps:
  - id: seed
    run: printf '{"items":["a","b"]}'
  - id: lane
    lanes: $seed.json.items
    batch_size: 2
    item_var: it
    steps:
      - id: s
        run: echo "$it"
`
	var p spec.Pipeline
	if err := yaml.Unmarshal([]byte(authored), &p); err != nil {
		t.Fatalf("decoding the authored pipeline: %v", err)
	}
	if len(p.Steps) != 2 {
		t.Fatalf("decoded steps = %d, want 2", len(p.Steps))
	}
	lane := p.Steps[1]
	if lane.Lanes != "$seed.json.items" || lane.BatchSize != 2 || lane.ItemVar != "it" {
		t.Fatalf("decoded lanes step = %+v, want lanes/batch_size/item_var intact", lane)
	}
	if err := ValidatePipeline(&p); err != nil {
		t.Fatalf("ValidatePipeline rejected the authored lanes step: %v", err)
	}
}

// TestValidatePipelineAcceptsLanes is the lanes twin of TestValidatePipelineAcceptsForEach,
// and it is an EXACT-EMPTINESS check for the same reason: a substring assertion would pass
// while a stray-companion error still rejected the pipeline. `lanes` is the CONCURRENT loop
// arm — it shares for_each's body/item_var/index_var shape and additionally REQUIRES the
// pool width (`batch_size`), so every accepted case below carries a positive one.
func TestValidatePipelineAcceptsLanes(t *testing.T) {
	cases := []struct {
		name string
		p    *spec.Pipeline
	}{
		{
			name: "lanes with the required width and a body",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "seed", Run: `printf '{"items":["a","b"]}'`},
				{Id: "lane", Lanes: "$seed.json.items", BatchSize: 2,
					Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}},
			}},
		},
		{
			name: "lanes with every companion",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "seed", Run: `printf '{"items":["a","b"]}'`},
				{Id: "lane", Lanes: "$seed.json.items", ItemVar: "it", IndexVar: "i", BatchSize: 3,
					Steps: []spec.PipelineSubStep{{Id: "s", Run: `echo "$it"`}}},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePipeline(tc.p); err != nil {
				t.Fatalf("ValidatePipeline rejected a valid %s pipeline: %v", tc.name, err)
			}
		})
	}
}

func TestValidatePipelineRules(t *testing.T) {
	cases := []struct {
		name string
		p    *spec.Pipeline
		want string
	}{
		{
			name: "two exec arms",
			p:    &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", Run: "true", Charly: []string{"status"}}}},
			want: "execution arms set",
		},
		{
			name: "no exec arm",
			p:    &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a"}}},
			want: "no execution arm",
		},
		{
			name: "for_each without steps",
			p:    &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", ForEach: "$x.y"}}},
			want: "for_each/lanes requires a non-empty steps list",
		},
		{
			name: "lanes without steps",
			p:    &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", Lanes: "$x.y", BatchSize: 2}}},
			want: "for_each/lanes requires a non-empty steps list",
		},
		{
			name: "lanes without the pool width",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "seed", Run: "true"},
				{Id: "a", Lanes: "$seed.json.items", Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}},
			}},
			want: "lanes requires batch_size >= 1 (the pool width)",
		},
		{
			name: "lanes with a pause",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "seed", Run: "true"},
				{Id: "a", Lanes: "$seed.json.items", BatchSize: 2, PauseMs: 100,
					Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}},
			}},
			want: "lanes cannot define pause_ms",
		},
		{
			name: "for_each and lanes are two arms",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "seed", Run: "true"},
				{Id: "a", ForEach: "$seed.json.items", Lanes: "$seed.json.items", BatchSize: 2,
					Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}},
			}},
			want: "2 execution arms set (for_each, lanes)",
		},
		{
			name: "lanes collection names a later step",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "a", Lanes: "$later.json.items", BatchSize: 2,
					Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}},
				{Id: "later", Run: "true"},
			}},
			want: `.lanes: reference $later.* names a step that is not declared earlier`,
		},
		{
			name: "steps without for_each",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a",
				Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}}}},
			want: "steps only apply alongside for_each",
		},
		{
			name: "item_var equals index_var",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", ForEach: "$x.y", ItemVar: "i", IndexVar: "i",
				Steps: []spec.PipelineSubStep{{Id: "s", Run: "true"}}}}},
			want: "item_var and index_var must differ",
		},
		{
			name: "input and approval together",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", Approval: "ok?",
				Input: spec.PipelineInput{Prompt: "p"}}}},
			want: "input and approval are mutually exclusive",
		},
		{
			name: "duplicate id in a parallel branch",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "a", Run: "true"},
				{Id: "p", Parallel: spec.PipelineParallel{Branches: []spec.PipelineSubStep{{Id: "a", Run: "true"}}}},
			}},
			want: "duplicate step id",
		},
		{
			name: "forward reference",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "a", When: "$b.ok", Run: "true"},
				{Id: "b", Run: "true"},
			}},
			want: "not declared earlier",
		},
		{
			name: "self reference",
			p:    &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", When: "$a.ok", Run: "true"}}},
			want: "not declared earlier",
		},
		{
			name: "plan reference to a later step",
			p: &spec.Pipeline{Steps: []spec.PipelineStep{
				{Id: "a", Plan: []spec.Step{{Run: "x", Op: spec.Op{Command: "echo $b.out"}}}},
				{Id: "b", Run: "true"},
			}},
			want: "plan: reference $b.* names a step that is not declared earlier",
		},
		{
			name: "when is not lobster grammar",
			p:    &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", When: "$x.y in [1]", Run: "true"}}},
			want: "is not lobster grammar",
		},
		{
			name: "parallel.wait with no branches",
			p:    &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "a", Run: "true", Parallel: spec.PipelineParallel{Wait: "any"}}}},
			want: "parallel.wait is only meaningful with non-empty parallel branches",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePipeline(tc.p)
			if err == nil {
				t.Fatalf("ValidatePipeline accepted an invalid pipeline (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ValidatePipeline error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidatePipelineReportsEveryProblem(t *testing.T) {
	p := &spec.Pipeline{Steps: []spec.PipelineStep{
		{Id: "a"},
		{Id: "b", Run: "true", Charly: []string{"status"}},
	}}
	err := ValidatePipeline(p)
	if err == nil {
		t.Fatal("expected errors")
	}
	if !strings.Contains(err.Error(), "no execution arm") || !strings.Contains(err.Error(), "execution arms set") {
		t.Errorf("ValidatePipeline must report EVERY problem, got %q", err)
	}
}

func TestValidateWorkflowCycles(t *testing.T) {
	if err := ValidateWorkflowCycles(map[string][]string{"a": {"b"}, "b": {"c"}, "c": {}}); err != nil {
		t.Fatalf("ValidateWorkflowCycles on an acyclic graph: %v", err)
	}
	err := ValidateWorkflowCycles(map[string][]string{"a": {"b"}, "b": {"a"}})
	if err == nil || !strings.Contains(err.Error(), "workflow cycle: ") {
		t.Fatalf("ValidateWorkflowCycles = %v, want a workflow cycle naming the chain", err)
	}
}
