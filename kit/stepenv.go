package kit

import (
	"regexp"
	"sort"
	"strings"
)

// stepenv.go — the DATA channel: a host's resolved values reach a command step's PROCESS
// ENVIRONMENT as data, never as shell source.
//
// The TEXT channel (ExpandOpVars) substitutes a value into a step's command TEXT, and the
// shell then PARSES that text — so a value that is DATA becomes SOURCE (a `$(…)` inside a
// value EXECUTES). The environment is the other channel: a value delivered as an environment
// variable is DATA, because the shell never re-parses the result of an expansion. A host
// that resolves values a step should SEE (not interpolate) hands them to the walk through
// the seam below, and the walk puts them in the step's env — which the command verb applies
// as a shell-QUOTED export, so the value arrives as one word no matter what it contains.

// StepEnvExporter is the OPTIONAL seam a host implements to hand the plan walk the values a
// step's PROCESS ENVIRONMENT should carry.
//
// It is deliberately NOT a method on PlanContext: that interface is public and its
// implementors live outside this repository, so a new method would break every consumer of
// the kit. A host that does not implement it keeps exactly the behaviour it has today —
// nothing is exported, and no verdict changes.
type StepEnvExporter interface {
	// StepEnv returns the values a step's process environment carries. The map is
	// READ-ONLY: the walk copies what it needs and never mutates the map it is given.
	StepEnv() map[string]string
}

// stepEnvNamePattern is the set of names a shell can export: a letter or underscore first,
// then letters, digits and underscores. A name outside it is NOT exportable —
// `export HOST_PORT:6379='v'` is a shell SYNTAX ERROR that would fail the whole step — and
// the check engine's own vocabulary (`HOST_PORT:6379`-style keys) is not the shell's.
var stepEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MergeStepEnv merges the host's exported values into the step's own env map and returns
// the MERGED COPY plus the names it could not export.
//
//   - The step's own `env:` WINS on a name collision: it is the author speaking last about
//     their own step, and the host's value is a default.
//   - The map is COPIED, never mutated in place. The walk's `expanded := *c` copies the Op
//     struct but SHARES the Env map, so mutating it would leak one run's values into the
//     next run of the same plan.
//   - A name that is not a valid shell identifier is NOT exported, and it is RETURNED, so
//     the caller can DISCLOSE it. A value that vanishes silently reads exactly like a host
//     that never supplied it, which is the failure mode this channel exists to remove.
//
// With nothing to export the step's own map is returned as-is: there is no merge to do, and
// the walk must not allocate a copy of every step's env for no reason.
func MergeStepEnv(stepEnv, exported map[string]string) (map[string]string, []string) {
	if len(exported) == 0 {
		return stepEnv, nil
	}
	merged := make(map[string]string, len(stepEnv)+len(exported))
	for k, v := range stepEnv {
		merged[k] = v
	}
	var rejected []string
	for k, v := range exported {
		if !stepEnvNamePattern.MatchString(k) {
			rejected = append(rejected, k)
			continue
		}
		if _, ok := merged[k]; ok {
			continue // the step's own env wins — the author spoke last about this step
		}
		merged[k] = v
	}
	sort.Strings(rejected)
	return merged, rejected
}

// StepEnvRejectionNote renders the names the data channel could not export, so a value the
// host handed over is never lost SILENTLY. It is a NOTE, not a verdict: a name the shell
// cannot export is a host-vocabulary mismatch (the check engine's `HOST_PORT:6379` keys are
// not the shell's), and the step itself still runs.
func StepEnvRejectionNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "not exported (not a valid shell identifier): " + strings.Join(names, ", ")
}
