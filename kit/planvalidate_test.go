package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestValidatePlanSteps_Diagnostics unit-tests ValidatePlanSteps — the SHARED plan-block
// validator both `charly box validate` (charly/validate.go) AND the externalized `charly
// feature validate` (via the "feature" HostBuild seam) invoke (relocated from
// charly/host_build_feature_test.go, K3 cone2 test closure: a pure sdk/kit function test with
// zero charly-core dependency, and no prior duplicate found in this package). It flags an empty
// description and an agent step that illegally carries an Op verb; a clean (empty) plan with a
// real description yields no errors.
func TestValidatePlanSteps_Diagnostics(t *testing.T) {
	// Empty description → flagged.
	if errs := ValidatePlanSteps("   ", nil, "candy:x"); len(errs) != 1 ||
		!strings.Contains(errs[0], "description is empty") {
		t.Fatalf("empty description: errs = %v, want exactly one 'description is empty'", errs)
	}

	// Non-empty description, no steps → clean.
	if errs := ValidatePlanSteps("a real description", nil, "candy:x"); len(errs) != 0 {
		t.Fatalf("clean: errs = %v, want none", errs)
	}

	// An agent-check step that carries an Op verb is illegal (agent steps must not). Setting
	// AgentCheck makes StepKind()==agent-check; setting the Op Plugin verb makes Kind() succeed.
	bad := spec.Step{AgentCheck: "the thing works"}
	bad.Plugin = "command"
	if errs := ValidatePlanSteps("desc", []spec.Step{bad}, "candy:x"); len(errs) != 1 ||
		!strings.Contains(errs[0], "agent steps must not carry an Op verb") {
		t.Fatalf("agent-step-with-verb: errs = %v, want the 'agent steps must not carry an Op verb' diagnostic", errs)
	}
}
