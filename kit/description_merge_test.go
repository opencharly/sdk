package kit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// description_merge_test.go — MergeDeployDescriptions replace-by-id semantics.
// Deploy-section replacement is the long-standing per-host overlay contract;
// cross-section replacement (Candy/Box) is the E-5 single-identity extension:
// a bed's local step with the same author id as a baked candy/box step now
// overshadows it IN PLACE (the local step runs at the baked step's position and
// is not appended a second time) — the fixture that stops the check-android-
// emulator-pod double appium session-create.

func ldOrigin(origin string, steps ...spec.Step) LabeledDescription {
	return LabeledDescription{Origin: origin, Plan: steps}
}

func stepWithID(id, verb string) spec.Step {
	return spec.Step{Check: "check " + verb, Op: spec.Op{ID: id, Plugin: verb}}
}

func TestMergeDeployDescriptions_EmptyLocalPlanReturnsBaked(t *testing.T) {
	baked := &LabelDescriptionSet{
		Candy: []LabeledDescription{ldOrigin("baked-candy", stepWithID("appium-session-create", "appium"))},
	}
	if got := MergeDeployDescriptions(baked, nil, "bed:x"); got != baked {
		t.Fatal("empty local plan must return the baked set unchanged")
	}
}

func TestMergeDeployDescriptions_AppendsWhenNoIDMatch(t *testing.T) {
	baked := &LabelDescriptionSet{
		Candy: []LabeledDescription{ldOrigin("baked-candy", stepWithID("baked-only", "adb"))},
	}
	got := MergeDeployDescriptions(baked, []spec.Step{stepWithID("fresh-step", "adb")}, "bed:x")
	if len(got.Candy) != 1 || got.Candy[0].Plan[0].ID != "baked-only" {
		t.Fatal("baked candy section must be untouched")
	}
	if len(got.Deploy) != 1 || got.Deploy[0].Origin != "deploy-local:bed:x" {
		t.Fatalf("local steps without a baked id match must append to a fresh deploy entry, got %+v", got.Deploy)
	}
	if got.Deploy[0].Plan[0].ID != "fresh-step" {
		t.Fatalf("appended step = %q, want fresh-step", got.Deploy[0].Plan[0].ID)
	}
}

func TestMergeDeployDescriptions_ReplacesBakedDeployStepInPlace(t *testing.T) {
	baked := &LabelDescriptionSet{
		Deploy: []LabeledDescription{ldOrigin("baked-deploy", stepWithID("appium-session-create", "appium"), stepWithID("keep-me", "adb"))},
	}
	local := []spec.Step{stepWithID("appium-session-create", "appium")}
	local[0].Op.Caps = "{local caps}"
	got := MergeDeployDescriptions(baked, local, "bed:x")
	if len(got.Deploy) != 1 {
		t.Fatalf("deploy entry count = %d, want 1 (replaced, not appended)", len(got.Deploy))
	}
	plan := got.Deploy[0].Plan
	if len(plan) != 2 {
		t.Fatalf("plan length = %d, want 2 (replacement keeps position, sibling stays)", len(plan))
	}
	if plan[0].ID != "appium-session-create" || plan[0].Op.Caps != "{local caps}" {
		t.Fatalf("first step not replaced by the local step: %+v", plan[0])
	}
	if plan[1].ID != "keep-me" {
		t.Fatal("unmatched sibling step must stay in place")
	}
}

// E-5: the local (fixture) session-create must REPLACE the baked CANDY step at
// its own position — one appium-session-create executes, leading the run, and
// the local step is not appended a second time.
func TestMergeDeployDescriptions_ReplacesBakedCandyStepInPlace(t *testing.T) {
	baked := &LabelDescriptionSet{
		Candy: []LabeledDescription{
			ldOrigin("baked-candy",
				stepWithID("av-before", "adb"),
				stepWithID("appium-session-create", "appium"),
				stepWithID("av-after", "appium"),
			),
		},
		Deploy: []LabeledDescription{ldOrigin("baked-deploy", stepWithID("status", "adb"))},
	}
	local := []spec.Step{stepWithID("status", "adb"), stepWithID("appium-session-create", "appium")}
	local[1].Op.Caps = "{the 3600s fixture caps}"
	got := MergeDeployDescriptions(baked, local, "check-android-emulator-pod")

	if len(got.Candy) != 1 {
		t.Fatalf("candy entries = %d, want 1", len(got.Candy))
	}
	plan := got.Candy[0].Plan
	if len(plan) != 3 {
		t.Fatalf("candy plan length = %d, want 3 (in-place replacement, no append)", len(plan))
	}
	if plan[1].ID != "appium-session-create" || plan[1].Op.Caps != "{the 3600s fixture caps}" {
		t.Fatalf("baked candy session-create not replaced in place: %+v", plan[1])
	}
	if plan[0].ID != "av-before" || plan[2].ID != "av-after" {
		t.Fatal("baked neighbors must keep their order around the replacement")
	}
	// The replaced local step must NOT be appended again, and neither may the
	// no-match local step if it replaced a Deploy step instead.
	if len(got.Deploy) != 1 || got.Deploy[0].Origin != "baked-deploy" {
		t.Fatalf("deploy must stay the baked entry only (the local 'status' replaced the baked deploy 'status' in place): %+v", got.Deploy)
	}
	if got.Deploy[0].Plan[0].ID != "status" {
		t.Fatal("baked deploy 'status' must be replaced by the local 'status'")
	}
}

func TestMergeDeployDescriptions_ReplacesBakedBoxStepInPlace(t *testing.T) {
	baked := &LabelDescriptionSet{
		Box: []LabeledDescription{ldOrigin("baked-box", stepWithID("appium-session-create", "appium"))},
	}
	local := []spec.Step{stepWithID("appium-session-create", "appium")}
	local[0].Op.Caps = "{local caps}"
	got := MergeDeployDescriptions(baked, local, "bed:x")
	if len(got.Box) != 1 || got.Box[0].Plan[0].Op.Caps != "{local caps}" {
		t.Fatalf("baked box step not replaced in place: %+v", got.Box)
	}
	if len(got.Deploy) != 0 {
		t.Fatalf("replaced local step must not append to deploy: %+v", got.Deploy)
	}
}

// E-5 neutralizer: a local step carrying skip:true with a baked step's id
// replaces that baked step — the RUNNER skips it (RunOne's c.Skip arm), which
// is the declarative "do not run this baked step for this bed" spelling.
func TestMergeDeployDescriptions_SkipReplacementNeutralizesBakedStep(t *testing.T) {
	baked := &LabelDescriptionSet{
		Candy: []LabeledDescription{ldOrigin("baked-candy", stepWithID("appium-session-delete", "appium"))},
	}
	local := []spec.Step{{
		Check: "check the E-5 fixture neutralizes the baked session-delete",
		Op:    spec.Op{ID: "appium-session-delete", Skip: true},
	}}
	got := MergeDeployDescriptions(baked, local, "check-android-emulator-pod")
	if len(got.Candy) != 1 || !got.Candy[0].Plan[0].Skip {
		t.Fatalf("baked step must be replaced by the skip:true neutralizer: %+v", got.Candy)
	}
	if len(got.Deploy) != 0 {
		t.Fatalf("neutralizer must not append to deploy: %+v", got.Deploy)
	}
}

func TestMergeDeployDescriptions_LastBakedIDWinsAcrossSections(t *testing.T) {
	baked := &LabelDescriptionSet{
		Candy:  []LabeledDescription{ldOrigin("baked-candy", stepWithID("dup", "adb"))},
		Deploy: []LabeledDescription{ldOrigin("baked-deploy", stepWithID("dup", "adb"))},
	}
	local := []spec.Step{stepWithID("dup", "adb")}
	local[0].Op.Caps = "{local caps}"
	got := MergeDeployDescriptions(baked, local, "bed:x")
	// Last-section wins: the Deploy occurrence is replaced (the Candy one stays baked).
	if len(got.Deploy) != 1 || got.Deploy[0].Plan[0].Op.Caps != "{local caps}" {
		t.Fatalf("deploy occurrence must be replaced: %+v", got.Deploy)
	}
	if got.Candy[0].Plan[0].Op.Caps != "" {
		t.Fatal("candy occurrence must stay untouched")
	}
}

func TestMergeDeployDescriptions_NilBaked(t *testing.T) {
	got := MergeDeployDescriptions(nil, []spec.Step{stepWithID("a", "adb")}, "bed:x")
	if got == nil || len(got.Deploy) != 1 || got.Deploy[0].Plan[0].ID != "a" {
		t.Fatalf("nil baked must produce a fresh deploy entry: %+v", got)
	}
}
