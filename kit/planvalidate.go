package kit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/spec"
)

// planvalidate.go — ValidatePlanSteps, the SHARED static plan-block validator
// (P12a: relocated from charly/plan_validate.go). It lives here — not
// candy/plugin-check — because it is invoked by BOTH `charly box validate`
// (charly/validate.go) AND the externalized `charly feature` command's "feature"
// HostBuild seam (charly/host_build_feature.go): CORE calls it directly at both
// sites, so it must be reachable without importing a plugin candy. One copy, R3.
//
//   - description non-empty
//   - every step has exactly one keyword (StepKind())
//   - run/check steps carry exactly one Op verb; agent-* steps carry none
//
// Returns a list of human-readable error strings (empty if OK).
func ValidatePlanSteps(desc string, plan []spec.Step, eid string) []string {
	var errs []string
	if strings.TrimSpace(desc) == "" {
		errs = append(errs, fmt.Sprintf("%s: description is empty", eid))
	}
	for i := range plan {
		step := plan[i]
		kw, err := step.StepKind()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: step %d: %v", eid, i, err))
			continue
		}
		switch kw {
		case KwRun, KwCheck:
			if _, verbErr := step.Kind(); verbErr != nil {
				errs = append(errs, fmt.Sprintf("%s: step %d (%s): %v", eid, i, kw, verbErr))
			}
		case KwAgentRun, KwAgentCheck:
			if _, verbErr := step.Kind(); verbErr == nil {
				errs = append(errs, fmt.Sprintf("%s: step %d (%s): agent steps must not carry an Op verb", eid, i, kw))
			}
		}
	}
	return errs
}
