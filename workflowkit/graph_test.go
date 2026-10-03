package workflowkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestGraphRenderings(t *testing.T) {
	p := &spec.Pipeline{Steps: []spec.PipelineStep{
		{Id: "fetch", Run: "true"},
		{Id: "probe", When: "$fetch.json.ok", Run: "true"},
		{Id: "fan", Parallel: spec.PipelineParallel{Branches: []spec.PipelineSubStep{
			{Id: "b1", Run: "true"},
			{Id: "b2", Stdin: "$fetch.json", Run: "true"},
		}}},
	}}

	edges := GraphEdges(p)
	want := map[string]bool{
		"fetch->probe(when)": true,
		"fan->b1(branch)":    true,
		"fan->b2(branch)":    true,
		"fetch->b2(stdin)":   true,
	}
	for _, e := range edges {
		delete(want, e.From+"->"+e.To+"("+e.Relation+")")
	}
	if len(want) != 0 {
		t.Errorf("GraphEdges is missing %v (got %v)", want, edges)
	}

	mermaid, err := RenderGraph(p, "mermaid")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(mermaid, "flowchart LR\n") || !strings.Contains(mermaid, "-->|when|") {
		t.Errorf("mermaid render:\n%s", mermaid)
	}

	dotSrc, err := RenderGraph(p, "dot")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dotSrc, "digraph") || !strings.Contains(dotSrc, "->") {
		t.Errorf("dot render:\n%s", dotSrc)
	}

	ascii, err := RenderGraph(p, "ascii")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ascii, "probe\n  <- fetch (when)\n") {
		t.Errorf("ascii render:\n%s", ascii)
	}

	if _, err := RenderGraph(p, "svg"); err == nil {
		t.Error("RenderGraph accepted an unknown format")
	}
}

func TestGraphNodesIncludeBranches(t *testing.T) {
	p := &spec.Pipeline{Steps: []spec.PipelineStep{
		{Id: "a", Run: "true"},
		{Id: "fan", Parallel: spec.PipelineParallel{Branches: []spec.PipelineSubStep{{Id: "b", Run: "true"}}}},
	}}
	got := strings.Join(GraphNodes(p), ",")
	if got != "a,fan,b" {
		t.Errorf("GraphNodes = %q", got)
	}
}
