package vmshared

import (
	"bytes"
	"testing"
)

// premiseOutcome is what the live-premise guard in TestQemuAliveLiveForeignQemu should do.
type premiseOutcome int

const (
	// premiseOK: the helper's argv is observable and carries the marker — run the assertion.
	premiseOK premiseOutcome = iota
	// premiseUnreadable: the helper is alive but its argv is not exposed (some runners mount
	// /proc so another process's cmdline reads back empty). A VISIBILITY limit, not a defect.
	premiseUnreadable
	// premiseBroken: the premise cannot be relied on — the helper exited, or its argv is readable
	// and does not carry the marker (so it is not running as qemu-system).
	premiseBroken
)

// qemuPremiseOutcome decides the guard's action from the two facts it can observe, with no
// dependency on a process, /proc, or the host — which is what makes the decision testable
// everywhere, including on the runners where the skip arm is the one that fires
// (opencharly/sdk#480).
func qemuPremiseOutcome(cmdline []byte, alive bool) premiseOutcome {
	if bytes.Contains(cmdline, []byte("qemu-system")) {
		return premiseOK
	}
	// An EMPTY cmdline is the INVISIBLE case: the runner does not expose another process's argv.
	// A NON-EMPTY cmdline without the marker is a different thing entirely — argv was readable and
	// the helper is not running as qemu-system, which means the premise really is broken and the
	// assertion below would be proving nothing. Conflating the two would let a genuinely broken
	// premise skip (opencharly/sdk#479 round 4).
	if alive && len(cmdline) == 0 {
		return premiseUnreadable
	}
	return premiseBroken
}

// TestQemuPremiseOutcome covers all three outcomes. Without it the unreadable-argv branch
// would be exercised only on a runner that hides argv, so it would ship unproven.
func TestQemuPremiseOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cmdline []byte
		alive   bool
		want    premiseOutcome
	}{
		{"observable argv with the marker", []byte("/tmp/x/qemu-system\x0030\x00"), true, premiseOK},
		{"alive but argv hidden", []byte(""), true, premiseUnreadable},
		{"alive, argv readable but NOT the marker — a broken premise, not an invisible one", []byte("/tmp/x/other\x00"), true, premiseBroken},
		{"helper gone", []byte(""), false, premiseBroken},
	} {
		if got := qemuPremiseOutcome(tc.cmdline, tc.alive); got != tc.want {
			t.Errorf("%s: qemuPremiseOutcome(%q, alive=%v) = %v, want %v",
				tc.name, tc.cmdline, tc.alive, got, tc.want)
		}
	}
}
