package deploykit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// skillBody builds the authored shape of one `skill:` node — only `owner` matters to this projection.
func skillBody(owner string) json.RawMessage {
	b, err := json.Marshal(map[string]any{
		"owner":       owner,
		"family":      "internals",
		"description": "d",
		"content":     "body",
	})
	if err != nil {
		panic(err)
	}
	return b
}

// TestCandyModelOwnedSkillsIsPerCandyNotPerBox is the coverage for opencharly/spec#254's projection
// step, and it asserts the one property that separates this from the per-image question:
//
// kind:skill's owned content (ops.OpContent) answers "which skills does THIS IMAGE carry?" and
// filters by the composed candy chain. This projects "which skills does THIS CANDY own?" onto the
// wire CandyModel — a per-CANDY question. The two must not be confused: a candy that no box composes
// still owns its skills, and a projection that copied the composed-chain filter would make
// `charly-internals` own its skills under one image and not under another.
//
// There is no cfg, no layers and no boxName in the signature — which is the property, stated by the
// type system rather than by a comment.
func TestCandyModelOwnedSkillsIsPerCandyNotPerBox(t *testing.T) {
	skills := map[string]json.RawMessage{
		"go":           skillBody("charly-internals"),
		"git-workflow": skillBody("charly-internals"),
		"local-deploy": skillBody("charly-local"),
		"concept-only": skillBody("charly-core"), // a concept candy: owns skills, composes into no image
	}

	got, err := CandyModelOwnedSkills(skills, "charly-internals")
	if err != nil {
		t.Fatalf("CandyModelOwnedSkills: %v", err)
	}
	if want := []string{"git-workflow", "go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("charly-internals owned skills = %v, want %v (sorted node names)", got, want)
	}

	// The concept-candy case, and it is the load-bearing one: nothing composes `charly-core` into an
	// image, and it still owns its skill. A projection that had reused the per-image composed-chain
	// filter would return nil here.
	got, err = CandyModelOwnedSkills(skills, "charly-core")
	if err != nil {
		t.Fatalf("CandyModelOwnedSkills(concept candy): %v", err)
	}
	if want := []string{"concept-only"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("charly-core owned skills = %v, want %v — a concept candy owns its skills even though no "+
			"box composes it (the per-CANDY vs per-BOX distinction)", got, want)
	}

	// A candy that owns nothing, and the absent-nodes case: both are nil, so the wire key is ABSENT
	// rather than an empty list.
	for _, tc := range []struct {
		name   string
		skills map[string]json.RawMessage
		candy  string
	}{
		{"candy owns nothing", skills, "charly-unrelated"},
		{"no skill nodes at all", nil, "charly-internals"},
		{"no candy name", skills, ""},
	} {
		got, err := CandyModelOwnedSkills(tc.skills, tc.candy)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != nil {
			t.Fatalf("%s: got %v, want nil", tc.name, got)
		}
	}
}

// TestCandyModelOwnedSkillsRejectsMalformedBody pins the failure mode: a body that does not decode is
// an ERROR, not a silent skip: a label entry or an installed body built from it would otherwise
// silently ship with its prose missing, and the same reasoning holds for the model — a model that quietly omitted a declared skill would be worse
// than one that failed, because nothing downstream could tell.
func TestCandyModelOwnedSkillsRejectsMalformedBody(t *testing.T) {
	skills := map[string]json.RawMessage{
		"good": skillBody("charly-internals"),
		"bad":  json.RawMessage(`{"owner": 42}`), // owner is not a string
	}
	if _, err := CandyModelOwnedSkills(skills, "charly-internals"); err == nil {
		t.Fatal("a skill body that does not decode must be an error, not a skip")
	}
}

// TestProjectCandy_ProjectsOwnedSkills pins the ONE candy projection both resolve scopes use: the
// candy's owned skill nodes ride its wire model. An import namespace's fold that bypassed it left
// an imported skill-owning candy looking empty (opencharly/sdk#598).
func TestProjectCandy_ProjectsOwnedSkills(t *testing.T) {
	c := NewSpecCandyModel(spec.CandyModel{Name: "carrier"}, spec.CandyView{Name: "carrier"})
	skills := map[string]json.RawMessage{
		"arch":   json.RawMessage(`{"family":"distros","owner":"carrier"}`),
		"other":  json.RawMessage(`{"family":"distros","owner":"someone-else"}`),
		"arch-b": json.RawMessage(`{"family":"distros","owner":"carrier"}`),
	}
	m, v, ok, err := ProjectCandy(c, "carrier", skills)
	if err != nil || !ok {
		t.Fatalf("ProjectCandy = ok %v, err %v", ok, err)
	}
	if v.Name != "carrier" {
		t.Fatalf("view name = %q, want the candy's", v.Name)
	}
	if strings.Join(m.Skills, ",") != "arch,arch-b" {
		t.Fatalf("model Skills = %q, want exactly the carrier's owned nodes, sorted", m.Skills)
	}

	if m, _, _, err := ProjectCandy(c, "carrier", nil); err != nil || m.Skills != nil {
		t.Fatalf("with no skill nodes: Skills = %q, err %v; want nil, nil", m.Skills, err)
	}
	if _, _, _, err := ProjectCandy(c, "carrier", map[string]json.RawMessage{"bad": json.RawMessage(`{"owner":`)}); err == nil {
		t.Fatal("a malformed skill node was projected silently; want an error")
	}
}

// TestUnionOwnedSkills pins the cross-scope merge: the authoring scope's ownership survives a scope
// that recorded the candy first with none, the union is sorted and de-duplicated, and an empty
// projection leaves the record untouched.
func TestUnionOwnedSkills(t *testing.T) {
	cases := []struct {
		recorded, projected, want []string
	}{
		{nil, []string{"b", "a"}, []string{"a", "b"}},
		{[]string{"a"}, []string{"c", "a"}, []string{"a", "c"}},
		{[]string{"a"}, nil, []string{"a"}},
		{nil, nil, nil},
	}
	for _, c := range cases {
		if got := UnionOwnedSkills(c.recorded, c.projected); !reflect.DeepEqual(got, c.want) {
			t.Errorf("UnionOwnedSkills(%q, %q) = %q, want %q", c.recorded, c.projected, got, c.want)
		}
	}
}
