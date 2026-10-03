package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// nested_desugar_test.go — the nested plan: desugar. A `kind: pipeline` body carries
// plan: step lists BELOW its top level (a `task: {plan: …}` nested task, a parallel
// branch, an inline entity), and each of them authors the SAME `<word>: <input>`
// plugin-verb sugar as the top-level plan:. The walk is kind-blind and stops only at a
// MEMBER-kind key — `task:` is NOT a member kind (it is not in spec.ResourceKinds), so an
// inline entity's plan IS desugared exactly like its siblings.
func TestNestedPlanDesugar(t *testing.T) {
	src := `
p1:
  pipeline:
    description: d
    steps:
      - id: a
        run: echo hi
      - id: b
        plan:
          - check: api up
            http: {url: "http://x"}
          - run: call task
            task: {task: hello}
      - id: c
        parallel:
          branches:
            - id: c1
              plan:
                - check: nested branch
                  http: {url: "http://y"}
    entities:
      inl:
        task:
          description: inline
          plan:
            - run: also desugared
              task: {task: other}
t1:
  task:
    description: t
    plan:
      - run: top
        task: {task: hello}
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	th := spec.Threaded{Kinds: map[string]bool{"pipeline": true, "task": true}}
	_, pp, err := ParseDoc(&doc, th)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	bodies := map[string]string{}
	for _, n := range pp.Nodes {
		bodies[n.Name] = string(n.Body)
		t.Logf("%s(%s): %s", n.Name, n.Disc, n.Body)
	}

	// p1's pipeline body: a top-level plan, a PARALLEL-BRANCH plan, and an inline entity's
	// plan all desugar.
	p1 := bodies["p1"]
	for _, want := range []string{
		`"plugin":"http","plugin_input":{"url":"http://x"}`, // step b, top-level plan
		`"plugin":"http","plugin_input":{"url":"http://y"}`, // step c, parallel branch plan
		`"plugin":"task","plugin_input":{"task":"hello"}`,   // step b, a verb sugar
		`"plugin":"task","plugin_input":{"task":"other"}`,   // the INLINE entity's plan
	} {
		if !strings.Contains(p1, want) {
			t.Errorf("p1: missing %s in %s", want, p1)
		}
	}
	if strings.Contains(p1, `"task":{"task":`) {
		t.Errorf("p1: an authored task: sugar survived the desugar: %s", p1)
	}
	if strings.Contains(p1, `"http":{`) {
		t.Errorf("p1: an authored http: sugar survived the desugar: %s", p1)
	}

	// t1 is a plain top-level task; its own top-level plan desugars too.
	if !strings.Contains(bodies["t1"], `"plugin":"task","plugin_input":{"task":"hello"}`) {
		t.Errorf("t1: top-level task plan not desugared: %s", bodies["t1"])
	}
}
