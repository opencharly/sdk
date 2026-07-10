package kit

import (
	"time"

	"github.com/opencharly/sdk/spec"
)

// CheckResult is the engine's record of running a single check: the verb's verdict
// (Status/Message/CapturedValue — the same three fields a verb returns as a Result) plus
// the engine bookkeeping a Result does not carry (which Op ran, its Verb, and the timing /
// retry accounting). The dispatch boundary builds a CheckResult from a verb's Result by
// stamping Op/Verb/Elapsed.
//
// Attempts and TotalElapsed are populated only when the check had an `eventually:` modifier
// (retry loop): Attempts=1 + TotalElapsed==Elapsed for a check that ran exactly once.
// Reporters surface these when Attempts>1 so slow startup paths are visible ("PASS in 5
// attempts over 12.3s").
type CheckResult struct {
	Op           *spec.Op
	Verb         string
	Status       Status
	Message      string
	Elapsed      time.Duration
	Attempts     int           `json:"attempts,omitempty"`
	TotalElapsed time.Duration `json:"total_elapsed,omitempty"`

	// DeadlineExceeded marks a result whose probe was killed by hitting its OWN
	// per-attempt deadline (probeNeverHang), NOT an external infra interruption. The
	// group-kill in runCaptureCmd surfaces the deadline SIGKILL as a signal-kill, so
	// without this flag the killed-probe retry (probeWasKilled) would futilely re-run a
	// probe that will only re-hang and re-hit the same deadline. An authoritative
	// "too slow" failure — never retried.
	DeadlineExceeded bool `json:"-"`

	// CapturedValue is the value stashed under `capture:` for consumption by downstream
	// steps in the same plan run. Empty when Capture was unset or the check did not pass
	// (captures are recorded only on final PASS — failing `eventually:` attempts don't
	// pollute).
	CapturedValue string `json:"captured_value,omitempty"`
}

// StepResult is one plan step's outcome — the step's identity (keyword/text/origin/id) plus
// the CheckResult of running it. The result reporters consume a []StepResult.
type StepResult struct {
	Keyword string      `json:"keyword"`
	Text    string      `json:"text"`
	Origin  string      `json:"origin,omitempty"`
	StepID  string      `json:"step_id"`
	Result  CheckResult `json:"result"`
}
