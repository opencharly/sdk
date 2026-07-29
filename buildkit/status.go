package buildkit

import "github.com/opencharly/spec/spec"

// status.go — the candy/box maturity-rung helpers relocated from charly/generate.go (the
// BUILD-cone cutover). Pure over spec.CandyReader; no loader/registry coupling.

// Status rungs. The default (empty) is StatusTesting; StatusWorking is the most permissive
// (used as the box-status seed so the candy chain drives the rung).
const (
	StatusWorking = "working"
	StatusTesting = "testing"
	StatusBroken  = "broken"
)

// ResolveStatus returns the effective status string. Empty defaults to StatusTesting. Accepts a
// single status word (working/testing/broken) — the legacy form used by older callers.
func ResolveStatus(s string) string {
	if s == "" {
		return StatusTesting
	}
	return s
}

// CandyStatus returns a candy's authored maturity rung (working|testing|broken), defaulting an
// unset value to StatusTesting. The authoritative per-candy status source.
func CandyStatus(c spec.CandyReader) string {
	if c == nil {
		return StatusTesting
	}
	return ResolveStatus(c.GetStatus())
}

// StatusSeverity returns a numeric severity for status comparison.
func StatusSeverity(s string) int {
	switch ResolveStatus(s) {
	case StatusWorking:
		return 0
	case StatusTesting:
		return 1
	case StatusBroken:
		return 2
	default:
		return 1 // unknown treated as testing
	}
}

// WorstStatus returns the more severe of two status values.
func WorstStatus(a, b string) string {
	if StatusSeverity(b) > StatusSeverity(a) {
		return ResolveStatus(b)
	}
	return ResolveStatus(a)
}
