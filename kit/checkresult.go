package kit

import (
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
//
// FLOOR-SLIM Unit 4 — the wire-envelope split: every field above is now spec.CheckResult
// (CUE-sourced, sdk/schema/checkresult.cue), EMBEDDED here. The ONE exception is
// DeadlineExceeded (below) — the spike-proven (P12) exception the wire mandate's own
// documented exception path authorizes: `json:"-"` (keep-in-Go, drop-from-wire) has no
// gengotypes construct. charly core's registry-coupled floor files (provider.go,
// provider_verb.go, verb_builtins.go, unified_targets.go, provider_checkenv.go) reference
// spec.CheckResult DIRECTLY (they never touch DeadlineExceeded) — zero new sdk/kit import.
// Field-promotion means every existing `.Status`/`.Op`/`.Verb`/`.Message`/`.Elapsed`/
// `.Attempts`/`.TotalElapsed`/`.CapturedValue` selector expression compiles UNCHANGED; only a
// composite LITERAL naming one of those fields must nest it under the embedded field name
// (`CheckResult{CheckResult: spec.CheckResult{Status: ...}, ...}`) — Go does not promote
// embedded-struct field names into a literal key position.
//
// The wire keys are deliberately snake_case now (op/verb/status/message/elapsed/…), a
// documented breaking change to `--format json`/TAP output — see sdk/schema/checkresult.cue
// and CHANGELOG for the old→new key mapping.
type CheckResult struct {
	spec.CheckResult

	// DeadlineExceeded marks a result whose probe was killed by hitting its OWN
	// per-attempt deadline (probeNeverHang), NOT an external infra interruption. The
	// group-kill in runCaptureCmd surfaces the deadline SIGKILL as a signal-kill, so
	// without this flag the killed-probe retry (probeWasKilled) would futilely re-run a
	// probe that will only re-hang and re-hit the same deadline. An authoritative
	// "too slow" failure — never retried. NEVER crosses the wire (see above).
	DeadlineExceeded bool `json:"-"`
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
