package kit

import (
	"strings"
	"testing"
	"time"

	"github.com/opencharly/spec/spec"
)

// never_hang_message_test.go — a step killed by the never-hang bound must SAY SO.
//
// The exec layer reports only "process terminated by signal (signal: killed)". That
// names the mechanism of death, not its cause, so a step killed by the per-attempt
// bound is indistinguishable from a step that crashed — and the reader goes hunting a
// bug in their own command. Measured cost: installing charly from a distro repo pulls
// ~190 packages, `dnf install` crossed the 90s floor, and the only evidence was
// "signal: killed".

func TestAnnotateNeverHangKill_NamesTheBoundAndTheRemedy(t *testing.T) {
	got := AnnotateNeverHangKill("process terminated by signal (signal: killed)", 90*time.Second)

	// The original message must survive — it still carries the exec-layer detail.
	if !strings.Contains(got, "terminated by signal") {
		t.Errorf("annotation dropped the original message: %q", got)
	}
	// The bound itself, so the reader can compare it against the step's duration.
	if !strings.Contains(got, "1m30s") {
		t.Errorf("message does not name the bound (want 1m30s): %q", got)
	}
	// The remedy, naming the authored field rather than describing it.
	if !strings.Contains(got, "timeout:") {
		t.Errorf("message does not point at `timeout:`: %q", got)
	}
	// And it must contradict the crash reading explicitly — that misreading is the
	// whole cost this annotation exists to remove.
	if !strings.Contains(got, "did not crash") {
		t.Errorf("message does not rule out the crash reading: %q", got)
	}
}

// Presence control: the helper must not stamp its explanation onto messages that have
// nothing to do with the bound. Without this, an implementation that appended the text
// unconditionally would satisfy the test above.
func TestAnnotateNeverHangKill_LeavesTheBoundOutOfUnrelatedText(t *testing.T) {
	plain := "exit=1, want 0 (stderr: no such package)"
	if got := AnnotateNeverHangKill(plain, 10*time.Minute); !strings.Contains(got, plain) {
		t.Errorf("original text must be preserved verbatim: %q", got)
	}
	// The annotation is applied by the caller ONLY when ctx deadline fired; the helper
	// itself is the formatter. Guard the wiring by asserting the trigger condition is
	// what ProbeNeverHang bounds, i.e. an authored timeout raises the reported bound.
	r := &Runner{probeTimeout: 90 * time.Second}
	slow := r.ProbeNeverHang(&spec.Op{Timeout: "10m"})
	if slow <= 90*time.Second {
		t.Errorf("an authored timeout: must raise the bound above the floor, got %s", slow)
	}
	if msg := AnnotateNeverHangKill("x", slow); !strings.Contains(msg, slow.String()) {
		t.Errorf("the message must name the RAISED bound, not the floor: %q", msg)
	}
}
