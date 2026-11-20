package deploykit

// description_vars_test.go — a candy's declared var: must reach the RESOLVED description.
//
// This is the leg that gives the check runner a producer. `BoxMetadata.Vars` has a
// consumer (kit's buildTimeVars) and no producer, because a container's metadata is
// decoded from OCI labels and `var:` is deliberately not one -- it is in
// DeployOnlyCapabilityFields for exactly that reason. The RESOLVED description is built
// from the project, so it is the shape where a declaration can actually travel.
//
// See opencharly/plugin-pipeline#70 for the six refuted hypotheses that led here, and
// opencharly/spec#413 for the field itself.

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCollectDescriptions_CarriesCandyVars pins that a candy declaring `var:` lands those
// names on its LabeledDescription, which is what a bed walks.
func TestCollectDescriptions_CarriesCandyVars(t *testing.T) {
	layers := map[string]spec.CandyReader{
		"plugin-x": NewSpecCandyModel(
			spec.CandyModel{Vars: map[string]string{
				"PROBE_FLAG": "from-the-candy-var",
				"RETRIES":    "3",
			}},
			spec.CandyView{Name: "plugin-x", Description: "a plugin that declares variables"},
		),
	}
	cfg := &spec.Config{Box: spec.BoxMap{
		"x-box": spec.EncodeBox(spec.BoxConfig{Candy: []string{"plugin-x"}}),
	}}

	set := CollectDescriptions(cfg, layers, "x-box")
	if set == nil || len(set.Candy) != 1 {
		t.Fatalf("CollectDescriptions = %+v, want one candy description", set)
	}
	vars := set.Candy[0].Vars
	if len(vars) != 2 {
		t.Fatalf("LabeledDescription.Vars = %v, want the 2 the candy declared -- a bed's "+
			"${NAME} has nothing to resolve against without them", vars)
	}
	if got := vars["PROBE_FLAG"]; got != "from-the-candy-var" {
		t.Errorf("PROBE_FLAG = %q, want from-the-candy-var", got)
	}
	if got := vars["RETRIES"]; got != "3" {
		t.Errorf("RETRIES = %q, want 3", got)
	}
}

// TestCollectDescriptions_VarsAloneStillCollects pins the guard: a candy that declares ONLY
// vars -- no description, no baked steps -- must still be described, or its variables would
// be silently dropped by the skip.
func TestCollectDescriptions_VarsAloneStillCollects(t *testing.T) {
	layers := map[string]spec.CandyReader{
		"vars-only": NewSpecCandyModel(
			spec.CandyModel{Vars: map[string]string{"ONLY": "this"}},
			spec.CandyView{Name: "vars-only"},
		),
	}
	cfg := &spec.Config{Box: spec.BoxMap{
		"v-box": spec.EncodeBox(spec.BoxConfig{Candy: []string{"vars-only"}}),
	}}

	set := CollectDescriptions(cfg, layers, "v-box")
	if set == nil || len(set.Candy) != 1 {
		t.Fatalf("CollectDescriptions = %+v, want the vars-only candy collected -- a candy "+
			"with variables and nothing else still has something a bed needs", set)
	}
	if got := set.Candy[0].Vars["ONLY"]; got != "this" {
		t.Errorf("ONLY = %q, want this", got)
	}
}
