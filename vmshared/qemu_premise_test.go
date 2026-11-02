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
	// premiseHelperGone: the helper has exited, so the premise really is broken.
	premiseHelperGone
)

// qemuPremiseOutcome decides the guard's action from the two facts it can observe, with no
// dependency on a process, /proc, or the host — which is what makes the decision testable
// everywhere, including on the runners where the skip arm is the one that fires
// (opencharly/sdk#480).
func qemuPremiseOutcome(cmdline []byte, alive bool) premiseOutcome {
	if bytes.Contains(cmdline, []byte("qemu-system")) {
		return premiseOK
	}
	if alive {
		return premiseUnreadable
	}
	return premiseHelperGone
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
		{"alive, argv present without the marker", []byte("/tmp/x/other\x00"), true, premiseUnreadable},
		{"helper gone", []byte(""), false, premiseHelperGone},
	} {
		if got := qemuPremiseOutcome(tc.cmdline, tc.alive); got != tc.want {
			t.Errorf("%s: qemuPremiseOutcome(%q, alive=%v) = %v, want %v",
				tc.name, tc.cmdline, tc.alive, got, tc.want)
		}
	}
}
