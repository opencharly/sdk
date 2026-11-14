package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCueKindDefs_HasKubevirt gates that the 6th deploy substrate is wired into
// the loader's kind→def table. The entry names the contract's ENVELOPE
// (#ResolvedKubeVirt) since opencharly/spec#304 unit 6 moved the authored body into the
// plugin that serves the kind — see cue_schema_kubevirt_test.go, which pins the name;
// this test pins that whatever the table names RESOLVES (the schema fail-fast panics if a
// cueKindDefs entry names an absent def).
func TestCueKindDefs_HasKubevirt(t *testing.T) {
	def, ok := cueKindDef("kubevirt")
	if !ok {
		t.Fatal("cueKindDefs[kubevirt] missing — the 6th substrate is not wired")
	}
	if !def.Exists() {
		t.Fatal("cueKindDefs[kubevirt] → def does not exist in the compiled schema")
	}
	if err := def.Err(); err != nil {
		t.Fatalf("cueKindDef(kubevirt) errored: %v", err)
	}
}

// TestDeployTraits_DrivesKubevirtStandalone asserts that the standalone-kind
// resolution is trait-driven for the new kind — a registered DeployTraits makes
// it a standalone template kind WITHOUT a kind-word switch (the boundary-law
// property the 6th substrate relies on).
func TestDeployTraits_DrivesKubevirtStandalone(t *testing.T) {
	tr := spec.Threaded{DeployTraits: map[string]*spec.DeployTraits{
		"kubevirt": {Venue: "ssh", MachineVenue: true, BedTarget: true},
	}}
	if !IsStandaloneResourceKind("kubevirt", tr) {
		t.Fatal("kubevirt with DeployTraits must be a standalone resource kind")
	}
	if tr.DeployTraits["kubevirt"].Venue != "ssh" {
		t.Fatalf("kubevirt venue = %q, want ssh", tr.DeployTraits["kubevirt"].Venue)
	}
	// A word with no trait entry is NOT standalone (the trait, not the word, is the gate).
	if IsStandaloneResourceKind("nosuchtrait", tr) {
		t.Fatal("a word with no DeployTraits must not resolve standalone")
	}
}
