package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// liveGroupThreaded models the LIVE host threading for the distro-arch corpus shape: `group` is
// a STRUCTURAL kind (plugin-group declares Structural:true, word "group") but NOT a deploy
// substrate (a group is targetless); `agent` is a threaded kind word colliding with a
// group-schema FIELD name (the live collision the charly worker RCA'd); and the declared-fields
// channel carries group's #GroupInput body fields — fed by the host from the REGISTERED schema.
var liveGroupThreaded = spec.Threaded{
	Kinds:            map[string]bool{"group": true, "check": true},
	DeploySubstrates: map[string]bool{"pod": true, "vm": true},
	StructuralKinds:  map[string]bool{"pod": true, "vm": true, "group": true, "agent": true},
	StructuralDeclaredFields: map[string]map[string]bool{
		"group": {"description": true, "disposable": true, "lifecycle": true, "iterate": true},
	},
}

// TestParse_StructuralDeclaredFieldStaysData is the corpus regression, under the CORRECT live
// threading (distro-arch charly.yml `check-agent-live`): a GROUP body's `iterate:` block
// carries an `agent:` key — a threaded kind WORD colliding with a group-schema FIELD name.
// Group is a STRUCTURAL parent, so the in-body scan runs the KEY rule: `iterate` is not a kind
// word → opaque data whose value is NEVER looked inside (no transitive recursion — the parse
// never sees the `agent:` key inside it), and it is also a DECLARED #GroupInput field (the
// channel). The iterate block travels as the group's own data, untouched; the only child is the
// deploy-level sibling watcher.
func TestParse_StructuralDeclaredFieldStaysData(t *testing.T) {
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
`), liveGroupThreaded)
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
	if len(pn.Children) != 1 || pn.Children[0].Name != "watcher" {
		t.Fatalf("children = %+v, want just the deploy-level sibling watcher", pn.Children)
	}
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

// TestParse_StructuralKindWordKeysAreMembers pins the no-declared-schema fallback (requirement
// b): a structural kind with NO declared schema threaded falls back to every kind-word key being
// a member — here the in-body `group:` key (a memberDisc word) carries a group entity under a
// group body.
func TestParse_StructuralKindWordKeysAreMembers(t *testing.T) {
	threaded := spec.Threaded{
		Kinds:            map[string]bool{"group": true},
		DeploySubstrates: map[string]bool{},
		StructuralKinds:  map[string]bool{"group": true},
	}
	_, pp, err := ParseDoc(docFrom(t, `
bed:
  group:
    description: outer group bed
    group:
      group:
        description: in-body member under a kind-word key
`), threaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	pn := pp.Nodes[0]
	if len(pn.Children) != 1 || pn.Children[0].Name != "group" || pn.Children[0].Disc != "group" {
		t.Fatalf("children = %+v, want the in-body kind-word-keyed group member", pn.Children)
	}
	body := bodyMap(t, pn.Body)
	if _, ok := body["group"]; !ok {
		t.Fatalf("authored body dropped the in-body member key: %v", body)
	}
}

// TestParse_StructuralDeclaredKindWordKeyStaysData pins the declared-fields channel's direct
// effect (requirement a): a structural kind whose registered schema declares a field NAMED like
// a kind word keeps that key as DATA — the channel decides, zero hardcoded lists.
func TestParse_StructuralDeclaredKindWordKeyStaysData(t *testing.T) {
	threaded := spec.Threaded{
		Kinds:            map[string]bool{"group": true},
		DeploySubstrates: map[string]bool{},
		StructuralKinds:  map[string]bool{"group": true},
		StructuralDeclaredFields: map[string]map[string]bool{
			"group": {"pod": true}, // a declared input-schema field NAMED like a kind word
		},
	}
	_, pp, err := ParseDoc(docFrom(t, `
bed:
  group:
    pod:
      sandbox: check-agent-pod
`), threaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes[0].Children) != 0 {
		t.Fatalf("children = %+v, want none — the declared kind-word field is data", pp.Nodes[0].Children)
	}
	body := bodyMap(t, pp.Nodes[0].Body)
	pod, ok := body["pod"].(map[string]any)
	if !ok || pod["sandbox"] != "check-agent-pod" {
		t.Fatalf("body.pod = %v, want the declared field intact as data", body["pod"])
	}
}
