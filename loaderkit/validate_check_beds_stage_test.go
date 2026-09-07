package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// Cutover C task 4 — all-or-nothing stage authoring per bed, enforced at LOAD.

// stageBedFixture builds a kind:check bed DeployNode with the given root plan
// and optional member sub-trees (both positions).
func stageBedFixture(rootPlan []spec.Step, members ...spec.Member) spec.DeployNode {
	disp := true
	return spec.DeployNode{
		Target:     "pod",
		Disposable: &disp,
		Plan:       rootPlan,
		Member:     members,
	}
}

// TestValidateBedStages_AllOrNothing pins the per-bed all-or-nothing invariant:
// a bed whose root or any member mixes staged and unstaged steps is rejected at
// load (the walk then never has to disambiguate — task 5 stages dominate).
func TestValidateBedStages_AllOrNothing(t *testing.T) {
	staged := func(s string) spec.Step { return spec.Step{Run: s, Op: spec.Op{Stage: "s1", Plugin: "p"}} }
	unstaged := func(s string) spec.Step { return spec.Step{Run: s, Op: spec.Op{Plugin: "p"}} }

	ok := func(name string) {
		t.Helper()
		node := stageBedFixture([]spec.Step{staged("a"), staged("b")},
			spec.Member{Name: "m1", Position: spec.PositionDeployLevel, Node: &spec.DeployNode{Plan: []spec.Step{staged("m1a")}}},
		)
		if err := validateBedStages(name, &node); err != nil {
			t.Fatalf("all-staged bed rejected: %v", err)
		}
		none := stageBedFixture([]spec.Step{unstaged("a"), unstaged("b")})
		if err := validateBedStages(name, &none); err != nil {
			t.Fatalf("all-unstaged bed rejected: %v", err)
		}
		empty := stageBedFixture(nil)
		if err := validateBedStages(name, &empty); err != nil {
			t.Fatalf("plan-less bed rejected: %v", err)
		}
	}

	reject := func(name string, node spec.DeployNode) {
		t.Helper()
		if err := validateBedStages(name, &node); err == nil {
			t.Fatalf("mixed bed accepted, want rejection")
		} else if !strings.Contains(err.Error(), "all-or-nothing") && !strings.Contains(err.Error(), "mixed stage") {
			t.Fatalf("rejection message does not name the all-or-nothing rule: %v", err)
		}
	}

	ok("bed-a")

	// root mixes staged + unstaged -> reject.
	reject("bed-root-mixed", stageBedFixture([]spec.Step{staged("a"), unstaged("b")}))

	// member mixes -> reject (per-bed scope spans the whole member tree).
	reject("bed-member-mixed", stageBedFixture([]spec.Step{staged("a")},
		spec.Member{Name: "m1", Position: spec.PositionDeployLevel, Node: &spec.DeployNode{Plan: []spec.Step{staged("m1a"), unstaged("m1b")}}},
	))

	// member tree recursively: an in-substrate grandchild mixed -> reject.
	reject("bed-grandchild-mixed", stageBedFixture([]spec.Step{staged("a")},
		spec.Member{Name: "m1", Position: spec.PositionInSubstrate, Node: &spec.DeployNode{
			Plan: []spec.Step{staged("m1a")},
			Member: []spec.Member{{Name: "m1b", Position: spec.PositionInSubstrate, Node: &spec.DeployNode{
				Plan: []spec.Step{staged("g1"), unstaged("g2")},
			}}},
		}},
	))
}

// TestValidateCheckBeds_StageAllOrNothingHooksIn pins the validator's hook into
// the real load-time entrypoint: a mixed bed fails ValidateCheckBeds (the same
// gate every command that resolves a bed runs through).
func TestValidateCheckBeds_StageAllOrNothingHooksIn(t *testing.T) {
	staged := spec.Step{Run: "a", Op: spec.Op{Stage: "s1", Plugin: "p"}}
	unstaged := spec.Step{Run: "b", Op: spec.Op{Plugin: "p"}}
	disp := true
	uf := &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{
		"bed": {Target: "pod", Disposable: &disp, Plan: []spec.Step{staged, unstaged}},
	}}
	if err := ValidateCheckBeds(uf, spec.Threaded{}); err == nil {
		t.Fatal("mixed-stage bed passed ValidateCheckBeds, want load-time rejection")
	}
}
