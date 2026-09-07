package loaderkit

// declared_fields_floor_test.go — the declared-fields CONTRACT FLOOR (the executor-boundary
// declared-fields channel, sdk #221/#223/#225 class). The declared-fields maps
// (spec.Threaded.DeployDeclaredFields / StructuralDeclaredFields) are HOST-process registry
// state: charly's loaderThreaded() fills them from pluginSchemas.inputDefs, whose population
// timing is command-dependent (the builtin schema gate runs inside loadProjectPlugins, mid-load).
// A parse that runs BEFORE the gate — the executor loader legs' walk (LoadUnifiedViaExecutor's
// "loader-walk" host leg), the deploy-target surfaces, or any plugin-side parse consumer —
// receives a Threaded whose declared-fields maps are EMPTY, and the documented
// no-declared-schema fallback re-arms: a converted tree's declared #Deploy fields re-scan as
// in-substrate members (the live repro: distro-arch check-agent-live's pod body —
// `iterate:` carrying an `agent:` kind-word key — failing `charly deploy add` with
// `node "sandbox": expected a mapping value, got yaml kind 8`).
//
// The design ruling: the declared-fields maps are CONTRACT DATA (what each kind schema
// declares), not host-registry state. For the spec-vocabulary kinds the declaration lives in
// the embedded spec/schema contract (the SAME single source cue:gen derives the Go types
// from), so the parse consults a CONTRACT FLOOR whenever the threaded map lacks the word:
// a degraded/early parse behaves IDENTICALLY to the fully-populated host-side parse. These
// tests pin that parity at the parse seam (the contract point every placement shares).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// degradedExecutorThreaded mirrors the Threaded an executor loader leg delivers when the
// host registry's schema gate has not run yet: kind/substrate recognition DATA is
// populated (provider registration is init-time), the declared-fields channels are EMPTY
// (pluginSchemas.inputDefs is population-lazy). This is the exact live-repro state.
func degradedExecutorThreaded() spec.Threaded {
	return spec.Threaded{
		Kinds:            map[string]bool{"agent": true},
		DeploySubstrates: map[string]bool{"pod": true, "vm": true},
		StructuralKinds:  map[string]bool{"agent": true},
		// DeployDeclaredFields / StructuralDeclaredFields deliberately ABSENT.
	}
}

// hostPopulatedThreaded mirrors the host-side parse state after the schema gate: the same
// recognition DATA plus the pod substrate's declared #Deploy field set (the served-schema
// channel fed by charly's loaderThreaded()).
func hostPopulatedThreaded() spec.Threaded {
	t := degradedExecutorThreaded()
	t.DeployDeclaredFields = map[string]map[string]bool{
		"pod": {
			"iterate": true, "plan": true, "disposable": true, "description": true,
			"agent_provisioned": true, "image": true, "box": true, "lifecycle": true,
		},
	}
	return t
}

// convertedTree is the converted-tree shape that live-reproduced the regression
// (distro-arch check-agent-live, hand-re-authored after the Cutover C group removal):
// a substrate body whose DECLARED `iterate:` field carries an `agent:` kind-word key
// (the ADE-iterate-bed collision class) whose own value shapes as a member.
const convertedTree = `
check-agent-live:
    pod:
        agent_provisioned: true
        description: Credential-sync projection for the disposable agent-control R10 bed.
        iterate:
            sandbox: check-agent-pod
            agent: [check-agent-live-claude]
            plateau_iteration: 1
            prompt: Reply with one short acknowledgement.
            note: false
            env: {}
`

func parseConvertedTree(t *testing.T, threaded spec.Threaded) spec.ParsedProject {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(convertedTree), &doc); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	_, pp, err := ParseDoc(&doc, threaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	return pp
}

// nodeJSON renders a ParsedProject deterministically for the RDD comparator.
func nodeJSON(t *testing.T, pp spec.ParsedProject) string {
	t.Helper()
	b, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestParse_DeclaredFieldsContractFloor_Substrate pins the floor: the DEGRADED executor
// Threaded (∅ declared-fields maps — the pre-gate registry state the executor legs deliver)
// parses the converted tree EXACTLY like the populated host-side parse — the declared
// `iterate:` field stays DATA (never looked inside), the KEY rule lives, and the
// live-repro `node "sandbox": expected a mapping value` parse error cannot re-arm.
func TestParse_DeclaredFieldsContractFloor_Substrate(t *testing.T) {
	pp := parseConvertedTree(t, degradedExecutorThreaded())
	if len(pp.Nodes) != 1 {
		t.Fatalf("expected 1 top-level node, got %d", len(pp.Nodes))
	}
	pn := pp.Nodes[0]
	if pn.Disc != "pod" {
		t.Fatalf("disc = %q, want pod", pn.Disc)
	}
	if len(pn.Children) != 0 {
		b, _ := json.Marshal(pn.Children)
		t.Fatalf("the declared iterate: field re-scanned as members — the floor is dead: %s", b)
	}
	// The iterate body stays intact in the opaque JSON body (the data channel).
	var body map[string]any
	if err := json.Unmarshal(pn.Body, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	it, ok := body["iterate"].(map[string]any)
	if !ok {
		t.Fatalf("iterate missing from the parsed body: %s", pn.Body)
	}
	if it["sandbox"] != "check-agent-pod" {
		t.Fatalf("iterate.sandbox = %v, want the scalar data intact", it["sandbox"])
	}
}

// TestParse_DeclaredFieldsContractFloor_Parity is the RDD parity proof: the degraded
// executor parse and the populated host-side parse of the SAME converted tree produce
// BYTE-IDENTICAL ParsedProject trees.
func TestParse_DeclaredFieldsContractFloor_Parity(t *testing.T) {
	degraded := nodeJSON(t, parseConvertedTree(t, degradedExecutorThreaded()))
	populated := nodeJSON(t, parseConvertedTree(t, hostPopulatedThreaded()))
	if degraded != populated {
		t.Fatalf("plugin-side (degraded) parse diverges from host-side (populated):\ndegraded:  %s\npopulated: %s", degraded, populated)
	}
}

// TestParse_DeclaredFieldsThreadedWins pins the presence-wins rule: a threaded entry
// (the served-schema channel, authoritative host-side) is never overridden by the floor.
// A registered def that deliberately OMITS the colliding field re-arms the plain value-shape
// scan — and the converted tree then fails with the canonical #221-class parse error, exactly
// as the populated host-side parse does against the same served def. The floor never widens a
// registered set; it only fills the ABSENT word (the pre-gate/degraded executor state).
func TestParse_DeclaredFieldsThreadedWins(t *testing.T) {
	tight := degradedExecutorThreaded()
	tight.DeployDeclaredFields = map[string]map[string]bool{
		"pod": {"plan": true}, // deliberately NOT iterate — the served def is authoritative
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(convertedTree), &doc); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	_, _, err := ParseDoc(&doc, tight)
	if err == nil {
		t.Fatalf("the threaded (served-schema) set must win: omitting iterate from the registered def must re-arm the #221-class value-shape scan")
	}
	if !strings.Contains(err.Error(), `node "sandbox"`) {
		t.Fatalf("expected the canonical #221-class parse error, got: %v", err)
	}
}

// TestParse_DeclaredFieldsContractFloor_UndeclaredStillMember is the negative control: the
// floor must not swallow REAL in-substrate members — a key that is NEITHER a declared
// contract field NOR anything else still classifies by the value-shape scan.
func TestParse_DeclaredFieldsContractFloor_UndeclaredStillMember(t *testing.T) {
	tree := convertedTree + `
    extra_member:
        vm:
            from: arch
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(tree), &doc); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	_, pp, err := ParseDoc(&doc, degradedExecutorThreaded())
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(pp.Nodes))
	}
	found := false
	for _, ch := range pp.Nodes[0].Children {
		if ch.Name == "extra_member" {
			found = true
		}
	}
	if !found {
		t.Fatalf("an undeclared entity-shaped body key must still classify as an in-substrate member")
	}
}

// TestContractDeclaredFields_Floor pins the floor's CONTENTS against the contract: every
// spec-vocabulary substrate word derives its declared field set from the embedded
// spec/schema (the cue:gen single source), covering the live-repro fields; an unknown
// (external plugin) word floors to nil — the documented no-declared-schema fallback is
// preserved for words the contract does not declare.
func TestContractDeclaredFields_Floor(t *testing.T) {
	for _, word := range []string{"pod", "vm", "local", "android", "kubernetes"} {
		fields := contractDeclaredFields(word)
		if len(fields) == 0 {
			t.Fatalf("contract floor for substrate %q is empty — the contract derivation is dead", word)
		}
	}
	pod := contractDeclaredFields("pod")
	for _, want := range []string{"iterate", "plan", "description", "agent_provisioned", "disposable"} {
		if !pod[want] {
			t.Fatalf("contract floor for pod lacks declared field %q (set: %v)", want, pod)
		}
	}
	if got := contractDeclaredFields("example-external-structkind"); got != nil {
		t.Fatalf("an external plugin word must floor to nil (documented fallback), got %v", got)
	}
}
