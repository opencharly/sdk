package deploykit

import (
	"encoding/json"
	"reflect"
	"testing"
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
// step, and it asserts the one property that separates this from CollectSkills:
//
// CollectSkills answers "which skills ride THIS IMAGE's label?" and filters by the composed candy
// chain, because a label must stay bounded. This projects "which skills does THIS CANDY own?" onto the
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
	// image, and it still owns its skill. A projection that had reused CollectSkills' composed-chain
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
// an ERROR, not a silent skip. CollectSkills already takes that position for the label ("a body that
// does not decode ... would otherwise silently ship a label entry with its prose missing"), and the
// same reasoning holds for the model — a model that quietly omitted a declared skill would be worse
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
