package kit

import (
	"fmt"
	"strings"

	"github.com/opencharly/spec/spec"
)

// planspec.go — the pure per-step spec helpers the plan walk needs: the keyword→do-mode
// dispatch, the stable step-id derivation, and the ${HOST:…} cross-member unresolved-var
// filter. All operate on spec.Step / plain strings, so they live in kit alongside the walk
// (planrun.go) that consumes them, shared with every plugin candy that runs a plan.

// HostVar is the cross-member address variable name — ${HOST:<member>} (+ optional :port).
// An unresolved one means the member is UNREACHABLE (a real failure, never a skip).
const HostVar = "HOST"

// StepDoMode maps the step keyword to the act/assert/instruct dispatch enum.
func StepDoMode(s *spec.Step) spec.DoMode {
	switch {
	case s.Run != "":
		return spec.DoAct
	case s.Check != "":
		return spec.DoAssert
	case s.AgentRun != "", s.AgentCheck != "":
		return spec.DoInstruct
	}
	return spec.DoAssert
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

// unresolvedName strips an expansion key's scoping suffix (`HOST:peer` → `HOST`) so every
// classifier compares the SAME name. One normaliser, one meaning (R3).
func unresolvedName(key string) string {
	if before, _, ok := strings.Cut(key, ":"); ok {
		return before
	}
	return key
}

// FilterHostVars returns the ${HOST:…} cross-member references among the unresolved keys. An
// unresolved ${HOST:…} means the member is unreachable — a real failure, never a SKIP (a skip
// on an unreachable dependency is a fake pass). Other unresolved vars (a deploy-only var under
// build scope, an unmounted volume) stay a legitimate skip — this function does not decide that;
// ClassifyUnresolved does.
func FilterHostVars(missing []string) []string {
	var out []string
	for _, key := range missing {
		if unresolvedName(key) == HostVar {
			out = append(out, key)
		}
	}
	return out
}

// UnresolvedClass says WHY a plan-step variable could not be resolved. The walk must not treat
// every unresolved name alike: one class is an honest skip, the other is an authoring error that
// would otherwise be handed to the operator as a PASS (opencharly/charly#865).
type UnresolvedClass int

const (
	// UnresolvedConditional — the host CAN supply this name in some OTHER mode or scope: a
	// deploy-scoped var under build scope, a var an unmounted volume would provide. The input
	// genuinely does not apply to THIS run, so a skip is the honest verdict.
	UnresolvedConditional UnresolvedClass = iota

	// UnresolvedUnknown — no mode or scope of this runner can supply the name to a step of this
	// kind. On an ASSERT-ONLY step that is an authoring error: a check's env is the runner's
	// auto-exports BY CONSTRUCTION, so the name can never resolve in ANY run — the assertion is
	// dead, and skipping it reads exactly like passing it.
	UnresolvedUnknown
)
