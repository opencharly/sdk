package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// groupCorpusThreaded threads the collision words the distro-arch corpus authors: the group +
// check kinds (host-threaded ClassKind words), the pod/vm deploy substrates, and the external
// `agent` structural kind word — whose NAME collides with the group schema's own `agent:`
// field (the exact live Threaded shape that broke charly's TestCueKinds_Corpus).
var groupCorpusThreaded = spec.Threaded{
	Kinds:            map[string]bool{"group": true, "check": true},
	DeploySubstrates: map[string]bool{"pod": true, "vm": true},
	StructuralKinds:  map[string]bool{"pod": true, "vm": true, "agent": true},
}

// TestParse_InBodyScan_OnlyNestingKinds is the corpus regression (distro-arch charly.yml
// `check-agent-live`, caught live by charly's TestCueKinds_Corpus after sdk #221): a GROUP
// body's `iterate:` block carries an `agent:` key — a structural kind WORD colliding with a
// group-schema FIELD name. In-body member detection must fire ONLY where member nesting is
// meaningful — a deploy-substrate body or an external structural body — never inside an
// arbitrary resource-kind body (group/check/candy/box) whose closed schema defines fields
// that can collide with kind words. The iterate block (scalar `sandbox:` included) must
// travel as the group's own data, untouched.
func TestParse_InBodyScan_OnlyNestingKinds(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
check-agent-live:
  group:
    description: Credential-sync projection for the disposable agent-control R10 bed.
    iterate:
      sandbox: check-agent-pod
      agent: [check-agent-live-claude]
      plateau_iteration: 1
      prompt: Reply with one short acknowledgement.
      note: false
      env: {}
  watcher:
    pod:
      image: img
`), groupCorpusThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(pp.Nodes))
	}
	pn := pp.Nodes[0]
	if pn.Disc != "group" {
		t.Fatalf("disc = %q, want group", pn.Disc)
	}
	// The iterate block must NOT classify as an in-body member; the ONLY child is the
	// deploy-level sibling watcher (group members are deploy-level, beside the disc key).
	if len(pn.Children) != 1 || pn.Children[0].Name != "watcher" {
		t.Fatalf("children = %+v, want just the deploy-level sibling watcher", pn.Children)
	}
	// The FULL authored body travels as data — the closed group schema owns every field.
	body := bodyMap(t, pn.Body)
	it, ok := body["iterate"].(map[string]any)
	if !ok {
		t.Fatalf("body.iterate = %v (%T), want the iterate mapping intact", body["iterate"], body["iterate"])
	}
	if it["sandbox"] != "check-agent-pod" || it["plateau_iteration"] != float64(1) {
		t.Fatalf("iterate data lost: %v", it)
	}
	agents, ok := it["agent"].([]any)
	if !ok || len(agents) != 1 || agents[0] != "check-agent-live-claude" {
		t.Fatalf("iterate.agent = %v, want the one-word list", it["agent"])
	}
}

// TestParse_InBodyScan_StructuralBodyStillScans pins the guard's positive arm: an external
// STRUCTURAL body still nests IN-BODY members (the parent-disc guard must not kill the
// structural channel the 8a fix opened — member nesting is meaningful there).
func TestParse_InBodyScan_StructuralBodyStillScans(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
parent:
  extstruct:
    cfg: x
    inner:
      extstruct:
        cfg: y
`), vmBedThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	pn := pp.Nodes[0]
	if pn.Disc != "extstruct" {
		t.Fatalf("disc = %q, want extstruct", pn.Disc)
	}
	if len(pn.Children) != 1 || pn.Children[0].Name != "inner" || pn.Children[0].Disc != "extstruct" {
		t.Fatalf("children = %+v, want the in-body extstruct member inner", pn.Children)
	}
	// The authored body keeps the in-body member key — the fold's position channel.
	body := bodyMap(t, pn.Body)
	if _, ok := body["inner"]; !ok {
		t.Fatalf("authored body dropped the in-body member key: %v", body)
	}
}
