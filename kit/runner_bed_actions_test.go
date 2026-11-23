package kit

import "testing"

// runner_bed_actions_test.go — the PRODUCTION accessor behind opencharly/plugin-check#153.
// The plan walk's exemption is `pc.VerifyOnly() && step.Mutates() && (!pc.BedActions() || !fs.bed)`,
// so the runner that a real `charly check run` builds is the only thing that makes a bed's
// own run: steps execute. A `NewRunner` that dropped `bedActions: cfg.BedActions` would leave
// every plan-walk test green (they drive a fake PlanContext) while production silently
// stayed verify-only — this asserts the real constructor, both arms.

func TestNewRunnerCarriesBedActions(t *testing.T) {
	on := NewRunner(RunnerConfig{VerifyOnly: true, BedActions: true})
	if !on.BedActions() {
		t.Fatal("NewRunner(RunnerConfig{BedActions: true}).BedActions() = false — a bed-actions run would skip the bed's own run: steps")
	}
	if !on.VerifyOnly() {
		t.Fatal("the runner must still report verify-only; bed actions are an exemption WITHIN it, not a mode switch")
	}
	off := NewRunner(RunnerConfig{VerifyOnly: true})
	if off.BedActions() {
		t.Fatal("NewRunner without BedActions must not execute a bed's mutating steps")
	}
}
