package workflowkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestValidatePipelineAcceptsAValidPipeline(t *testing.T) {
	if err := ValidatePipeline(e2ePipeline()); err != nil {
		t.Fatalf("ValidatePipeline rejected a valid pipeline: %v", err)
	}
	if err := ValidatePipeline(nil); err == nil {
		t.Error("ValidatePipeline accepted nil")
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
			want: "for_each requires a non-empty steps:",
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
