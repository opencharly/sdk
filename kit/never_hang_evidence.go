package kit

import (
	"fmt"
	"strings"
	"time"
)

// never_hang_evidence.go — the per-attempt never-hang kill must name WHAT WAS ABSENT,
// not only which bound fired.
//
// The bound alone answers "how long did it run"; it does not answer "was the probe
// making progress, or was it wedged?" — and those two have different owners: a wedged
// probe is a defect in the probe, a slow-but-progressing probe needs an authored
// `timeout:`. Where a LOCAL pid is known, the process itself is the witness: it is
// either still alive (so the kill cut short a probe that was still running and had
// produced no output since the observation started) or already gone (so the bound was
// not what ended it). Saying which is the difference between a diagnosable kill and a
// dead end.
//
// Remote/venue probes have no local pid to read, so they report Observed=false and keep
// exactly the message they had before — the qualifier the design draws is "where a local
// pid exists", not "everywhere".
type KillEvidence struct {
	// Bound is the per-attempt never-hang ceiling that fired.
	Bound time.Duration
	// Observed reports whether a local progress observation was available at kill time.
	Observed bool
	// Pid is the local process that was running the probe (0 when unknown).
	Pid int
	// Alive reports whether that process was still running when the bound fired.
	Alive bool
	// SilentFor is how long the probe had produced no progress when the bound fired.
	SilentFor time.Duration
}

// ObserveLocalKill builds the evidence for a local pid: whether it is still alive and how
// long it has been silent. `alive` and `now` are injected so a caller (and a test) can
// read liveness and time deterministically — the seam that lets the never-hang path be
// proven without waiting for a real bound to elapse.
func ObserveLocalKill(pid int, silentSince, now time.Time, alive func(int) bool) KillEvidence {
	ev := KillEvidence{Observed: true, Pid: pid, SilentFor: now.Sub(silentSince)}
	if alive != nil {
		ev.Alive = alive(pid)
	}
	return ev
}

// AnnotateNeverHangKillWithEvidence appends the bound-and-remedy line the never-hang kill
// always carries, and — when a local observation exists — one more line naming the absent
// signal: the process was still alive and silent, or it was already gone. It is the ONE
// implementation; AnnotateNeverHangKill is its unobserved case.
func AnnotateNeverHangKillWithEvidence(msg string, ev KillEvidence) string {
	out := strings.TrimSpace(fmt.Sprintf(
		"%s\nkilled by the per-attempt never-hang bound of %s — the step ran longer than that, "+
			"it did not crash. If it is legitimately this slow, declare `timeout:` on the step "+
			"(a longer value is honoured over the bound).", msg, ev.Bound))
	if !ev.Observed {
		return out
	}
	state := "had already exited"
	if ev.Alive {
		state = "was still alive"
	}
	return strings.TrimSpace(fmt.Sprintf(
		"%s\nno progress observed for %s: the probe's local process (pid %d) %s when the bound fired.",
		out, ev.SilentFor.Round(time.Second), ev.Pid, state))
}
