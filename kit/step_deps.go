package kit

import "github.com/opencharly/sdk/spec"

// step_deps.go — the step-level dependency helper the check runner uses to short-circuit a step
// whose declared deps have not passed. Moved from charly/step_topo.go (CHECK-wave): pure over
// spec.Step + a verdict map, zero core state, so it lives in sdk/kit like every other
// vocabulary-independent plan-walk helper (mirrors planrun.go's RunPlan). Its sole caller,
// charly/check_runner_live.go, stays in core (loader-resolved deploy roots, K1) — only this pure
// function moved, not its caller.

// FirstUnmetDepStep returns the first dep id in s.DependsOn whose verdict is anything other than
// "pass" (or that is unknown / not yet run). Returns "" if every dep passed (or the step has no
// deps).
func FirstUnmetDepStep(s spec.Step, verdictByID map[string]string) string {
	for _, dep := range s.DependsOn {
		v, ok := verdictByID[dep]
		if !ok || v != "pass" {
			return dep
		}
	}
	return ""
}
