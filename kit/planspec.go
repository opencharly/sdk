package kit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/spec"
)

// planspec.go — the pure per-step spec helpers the plan walk needs: the keyword→do-mode
// dispatch, the stable step-id derivation, and the ${HOST:…} cross-member unresolved-var
// filter. All operate on spec.Step / plain strings, so they live in kit alongside the walk
// (planrun.go) that consumes them, shared with every plugin candy that runs a plan.

// HostVar is the cross-member address variable name — ${HOST:<member>} (+ optional :port).
// An unresolved one means the member is UNREACHABLE (a real failure, never a skip).
const HostVar = "HOST"

// StepDoMode maps the step keyword to the act/assert/instruct dispatch enum.
func StepDoMode(s *spec.Step) DoMode {
	switch {
	case s.Run != "":
		return DoAct
	case s.Check != "":
		return DoAssert
	case s.AgentRun != "", s.AgentCheck != "":
		return DoInstruct
	}
	return DoAssert
}

// StepID returns the stable identifier used for plan-overlay merge lookups, depends_on
// references, and ${STEP_ID} substitution — a deterministic id derived from origin + position.
func StepID(origin string, stepIdx int) string {
	return fmt.Sprintf("plan:%s:%d", origin, stepIdx)
}

// EffectiveStepID returns the step's author id when set, else a derived id.
func EffectiveStepID(s *spec.Step, origin string, stepIdx int) string {
	if s.ID != "" {
		return s.ID
	}
	return StepID(origin, stepIdx)
}

// FilterHostVars returns the ${HOST:…} cross-member references among the unresolved keys. An
// unresolved ${HOST:…} means the member is unreachable — a real failure, never a SKIP (a skip
// on an unreachable dependency is a fake pass). Other unresolved vars (a deploy-only var under
// build scope, an unmounted volume) stay a legitimate skip.
func FilterHostVars(missing []string) []string {
	var out []string
	for _, key := range missing {
		name := key
		if before, _, ok := strings.Cut(key, ":"); ok {
			name = before
		}
		if name == HostVar {
			out = append(out, key)
		}
	}
	return out
}
