// step_typing_on_load_test.go — TODO 3.3 (§12.1, D-LOAD-5): a plan step's typed form is judged on
// EVERY load, not only by `charly box validate`.
//
// MEASURED BEFORE WRITING THIS (both halves driven, same document, same Threaded snapshot):
//
//	load            -> <nil>
//	box validate    -> measure: redis: plan step 0: plan.0: 5 errors in empty disjunction:
//
// so the desugar (desugar_steps.go) judges a step's INTENT KEYWORD — an unknown verb is already
// refused at load — while the step's BODY (`- run: 42`, or `run:` carrying a field the closed
// #Step does not define) is typed ONLY by ValidateNodeFormSteps, which only `charly box validate`
// runs. That is the gap this test pins.
//
// ParseDocStream is the ONE per-document pipeline the FILE WALK and the binary-embedded
// vocabulary stream both drive, so "every load" has exactly one home there.
package loaderkit

import (
	"strings"
	"testing"
)

// TestParseDocStream_TypesStepBodiesOnLoad: a run: step whose body the closed #Step/#Op does not
// admit is refused by the composer at load time, naming the document it came from — the same
// verdict `charly box validate` already reaches, reached before any runner can dispatch the step.
func TestParseDocStream_TypesStepBodiesOnLoad(t *testing.T) {
	seams, _ := streamSeams(t)
	data := []byte("redis:\n  candy:\n    plan:\n      - run: 42\n")
	_, _, _, err := ParseDocStream(data, "step-typing", "srcdir", seams)
	if err == nil {
		t.Fatal("ParseDocStream accepted a run: step whose body the closed #Step does not admit")
	}
	if !strings.Contains(err.Error(), "step-typing") {
		t.Fatalf("error does not name the document it came from: %v", err)
	}
	if !strings.Contains(err.Error(), "plan step 0") {
		t.Fatalf("error does not name the offending step: %v", err)
	}
}
