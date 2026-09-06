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

// livePodThreaded models the LIVE host threading for the canonical charly-cli iterate-entity bed
// shape: `pod` is BOTH a deploy substrate AND a structural kind (the live threading), `agent`
// is a threaded kind word colliding with values inside a declared #Deploy field's body, and the
// deploy-declared-fields channel carries the #Deploy body field names — fed by the host from the
// REGISTERED substrate schema (the #Deploy family). The fixture carries the #Deploy fields the
// collision class actually hits (plus the cross-ref spine); it is host-fed DATA, not the
// mechanism's own vocabulary.
var livePodThreaded = spec.Threaded{
	Kinds:            map[string]bool{"pod": true, "agent": true, "check": true},
	DeploySubstrates: map[string]bool{"pod": true, "vm": true},
	StructuralKinds:  map[string]bool{"pod": true, "vm": true, "agent": true},
	DeployDeclaredFields: map[string]map[string]bool{
		"pod": {
			"from": true, "image": true, "env": true, "disposable": true,
			"plan": true, "iterate": true, "record": true, "instrument": true,
			"cpus": true, "ram": true, "disk_size": true, "snapshot": true,
			"update_gate": true, "agent_provisioned": true,
		},
	},
}

// TestParse_SubstrateDeclaredIterateStaysData is THE sdk #221 parse-regression repro, under the
// CORRECT live threading: a POD primary body (a deploy substrate) carrying the canonical ADE
// iterate pattern — iterate: {sandbox: check-agent-pod, agent: [check-agent-live-claude],
// plateau_iteration: 1, prompt: ..., note: false, env: {}} — plus a scalar sibling field. The
// substrate branch's value-shape scan used to classify `iterate:` as an IN-SUBSTRATE MEMBER
// named "iterate" purely because its value mapping carries the kind-word key `agent:`; then
// parseNode("iterate") took `agent` as ITS disc and the scalar `sandbox:` hard-failed
// "node sandbox: expected a mapping value, got yaml kind 8". Now: `iterate` is a DECLARED
// #Deploy field (the channel), so it is DATA — its value is never looked inside; the pod still
// parses; and the deploy-level sibling member still classifies.
func TestParse_SubstrateDeclaredIterateStaysData(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
check-agent-live:
  pod:
    image: sandbox-img
    iterate:
      sandbox: check-agent-pod
      agent: [check-agent-live-claude]
      plateau_iteration: 1
      prompt: Reply with one short acknowledgement.
      note: false
      env: {}
  watcher:
    pod:
      image: watcher-img
`), livePodThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(pp.Nodes))
	}
	pn := pp.Nodes[0]
	if pn.Disc != "pod" {
		t.Fatalf("disc = %q, want pod", pn.Disc)
	}
	// The only member is the deploy-level sibling watcher — iterate is DATA, not a member.
	if len(pn.Children) != 1 || pn.Children[0].Name != "watcher" {
		t.Fatalf("children = %+v, want just the deploy-level sibling watcher", pn.Children)
	}
	// The full iterate body travels intact as the pod's own data.
	body := bodyMap(t, pn.Body)
	it, ok := body["iterate"].(map[string]any)
	if !ok {
		t.Fatalf("body.iterate = %v (%T), want the iterate mapping intact", body["iterate"], body["iterate"])
	}
	if it["sandbox"] != "check-agent-pod" || it["plateau_iteration"] != float64(1) || it["note"] != false {
		t.Fatalf("iterate data lost: %v", it)
	}
	agents, ok := it["agent"].([]any)
	if !ok || len(agents) != 1 || agents[0] != "check-agent-live-claude" {
		t.Fatalf("iterate.agent = %v, want the one-word list", it["agent"])
	}
	if _, ok := it["env"].(map[string]any); !ok {
		t.Fatalf("iterate.env = %v, want the empty mapping", it["env"])
	}
}

// TestParse_SubstrateDeclaredKindWordKeyStaysData pins the deploy channel's direct
// kind-word-collision proof: a substrate whose registered schema declares a field NAMED like a
// kind word (here a declared field literally named `agent`) keeps that key as DATA even though
// the key itself is a memberDisc word — the channel decides, zero hardcoded lists.
func TestParse_SubstrateDeclaredKindWordKeyStaysData(t *testing.T) {
	// A substrate whose registered schema declares a field NAMED like a kind word: the live
	// #Deploy schema has no `agent` field (the fixture above models it), so this test threads
	// its own channel entry declaring one — the channel decides, not a word list.
	threaded := livePodThreaded
	threaded.DeployDeclaredFields = map[string]map[string]bool{
		"pod": {"agent": true},
	}
	_, pp, err := ParseDoc(docFrom(t, `
bed:
  pod:
    image: img
    agent:
      pod:
        image: agent-img
`), threaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes[0].Children) != 0 {
		t.Fatalf("children = %+v, want none — the declared kind-word field is data", pp.Nodes[0].Children)
	}
	body := bodyMap(t, pp.Nodes[0].Body)
	agent, ok := body["agent"].(map[string]any)
	if !ok || agent["pod"].(map[string]any)["image"] != "agent-img" {
		t.Fatalf("body.agent = %v, want the declared field intact as data", body["agent"])
	}
}

// TestParse_SubstrateNonDeclaredKeysStillMembers pins the scan's other half: a kind-word key
// that is NOT a declared #Deploy field, and a plain entity-keyed value-shape member, both still
// classify as in-substrate members under the LIVE threading (the channel only excludes declared
// fields — web:/chrome:/sidecar: style members stay members).
func TestParse_SubstrateNonDeclaredKeysStillMembers(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
bed:
  pod:
    image: img
    sidecar:
      pod:
        image: sidecar-img
    agent:
      pod:
        image: agent-img
`), livePodThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	pn := pp.Nodes[0]
	if len(pn.Children) != 2 || pn.Children[0].Name != "sidecar" || pn.Children[1].Name != "agent" {
		t.Fatalf("children = %+v, want [sidecar, agent] in authored order", pn.Children)
	}
	if pn.Children[0].Disc != "pod" || pn.Children[1].Disc != "pod" {
		t.Fatalf("member discs = %q/%q, want pod/pod", pn.Children[0].Disc, pn.Children[1].Disc)
	}
}

// TestParse_SubstrateNoDeclaredSchemaFallback pins the documented no-declared-schema fallback:
// a substrate word with NO deploy-declared-fields entry keeps the plain value-shape scan —
// the entity-keyed in-body member classifies exactly as before the channel existed.
func TestParse_SubstrateNoDeclaredSchemaFallback(t *testing.T) {
	threaded := spec.Threaded{
		Kinds:            map[string]bool{"pod": true, "exampledeploy": true},
		DeploySubstrates: map[string]bool{"pod": true, "exampledeploy": true},
		StructuralKinds:  map[string]bool{"pod": true, "exampledeploy": true},
	}
	_, pp, err := ParseDoc(docFrom(t, `
bed:
  exampledeploy:
    from: golden
    sidecar:
      pod:
        image: img
`), threaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	pn := pp.Nodes[0]
	if len(pn.Children) != 1 || pn.Children[0].Name != "sidecar" || pn.Children[0].Disc != "pod" {
		t.Fatalf("children = %+v, want the value-shape sidecar member unchanged", pn.Children)
	}
	body := bodyMap(t, pn.Body)
	if _, ok := body["sidecar"]; !ok {
		t.Fatalf("authored body dropped the in-body member key: %v", body)
	}
}

