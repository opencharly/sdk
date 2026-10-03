package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// A CORE resource kind (spec.ResourceKinds — the deployable kinds whose #Node arm nests a
// sub-entity child) is a substrate PARENT even when the host has not threaded it into
// DeploySubstrates. DeploySubstrates is filled from the host's out-of-process deploy providers
// plus the parse-time prescan, so before those connect a core word such as `vm` is absent from
// it while still being a StructuralKind; the parse then fell to the structural KEY rule, which
// can never match an in-body member (its KEY is the member's own name, the kind word lives in
// its VALUE), so the member was skipped as opaque data and left in the body for a closed
// #<Kind>Value gate to reject (#748).
func TestParseCoreResourceKindIsSubstrateParent(t *testing.T) {
	doc := `
check-group:
  vm:
    description: group bed
    check-group-member:
      local:
        from: check-group-app
`
	var ydoc yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &ydoc); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	// The regression condition: the word is threaded as a STRUCTURAL kind and NOT as a
	// deploy substrate — the shape the corpus test runs with.
	th := spec.Threaded{
		DeploySubstrates: map[string]bool{},
		StructuralKinds:  map[string]bool{"vm": true, "local": true},
	}
	_, pp, err := ParseDoc(&ydoc, th)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(pp.Nodes))
	}
	n := pp.Nodes[0]
	if len(n.Children) != 1 {
		t.Fatalf("node %q: want 1 member child, got %d (body=%s)", n.Name, len(n.Children), n.Body)
	}
	if got := n.Children[0].Name; got != "check-group-member" {
		t.Errorf("member child name = %q, want check-group-member", got)
	}
	if got := n.Children[0].Disc; got != "local" {
		t.Errorf("member child disc = %q, want local", got)
	}
	// The authored body KEEPS the member key — it is the position channel the fold stamps
	// Member.Position from, so the parse must not drop it here.
	if !strings.Contains(string(n.Body), "check-group-member") {
		t.Errorf("authored body lost the member key (the position channel): %s", n.Body)
	}
	// The emitter, by contrast, MUST omit it — the member tree owns that key.
	emitted, err := EntityBodyJSON(n)
	if err != nil {
		t.Fatalf("EntityBodyJSON: %v", err)
	}
	if strings.Contains(string(emitted), "check-group-member") {
		t.Errorf("emitted body still carries the member key: %s", emitted)
	}
}
