package workflowkit

// planrefs_test.go — the direct contract of the two EXPORTED plan-ref steps.
//
// Before they were exported, `planRefs`/`rewritePlanRefs` were reached only THROUGH
// `Lower`, so the lowering goldens were their only witness. They are public API now
// (opencharly/spec#304 unit 1, whose relocation is why), and one property they carry is
// invisible in a golden: the rewrite is a SINGLE leftmost-longest pass, which is why a
// `$a.x` reference cannot eat the prefix of a `$a.x.y` one. A map-based "replace each key" alternative — the obvious hand-rolled
// shortcut — passes every golden and corrupts exactly that case, so it is pinned here.

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// planNode parses a plan into the yaml.Node shape the lowering walks: the SEQUENCE a
// step's `plan:` maps to, never the document wrapper around it.
func planNode(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
		t.Fatalf("parsing the plan fixture: %v", err)
	}
	if n.Kind != yaml.DocumentNode || len(n.Content) != 1 {
		t.Fatalf("the plan fixture did not parse to a document with one node")
	}
	return n.Content[0]
}

// TestPlanRefsDistinctInDocumentOrder pins the collection rule: every `$step.path` in a
// plan, deduplicated, in DOCUMENT order — and mapping KEYS are never a place a reference
// lives (a key named `$a.x` must not be collected).
func TestPlanRefsDistinctInDocumentOrder(t *testing.T) {
	plan := planNode(t, `
- run: echo $b.two $a.one
- check: test -n "$b.two"
- run: echo $c.three $a.one
`)
	got := PlanRefs(plan)
	want := []string{"$b.two", "$a.one", "$c.three"}
	if len(got) != len(want) {
		t.Fatalf("PlanRefs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PlanRefs = %v, want %v (document order, deduplicated)", got, want)
		}
	}

	keyed := planNode(t, "- $a.one: echo $b.two\n")
	if got, want := PlanRefs(keyed), []string{"$b.two"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("PlanRefs(key position) = %v, want %v (a KEY is not a reference site)", got, want)
	}
}

// TestRewritePlanRefsOnePassAndBraceForm pins the two properties the emitted artifact
// depends on: the replacement is the BRACE form (`${REF_n}`, the only spelling the plan
// expander reads) and it happens in ONE leftmost-longest pass, so a reference that is a
// strict PREFIX of another both survive with their own ordinal.
func TestRewritePlanRefsOnePassAndBraceForm(t *testing.T) {
	plan := planNode(t, "- run: echo $a.x $a.x.y\n")
	refs := PlanRefs(plan)
	if len(refs) != 2 {
		t.Fatalf("PlanRefs = %v, want both the prefix ref and the longer ref", refs)
	}
	RewritePlanRefs(plan, refs)

	if got, want := plan.Content[0].Content[1].Value, "echo ${REF_1} ${REF_2}"; got != want {
		t.Fatalf("rewritten plan = %q, want %q", got, want)
	}
}
