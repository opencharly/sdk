package kit

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"
)

// never_hang_evidence_test.go — a killed probe must say WHAT WAS ABSENT, not only which
// bound fired, and the reading must be provable without waiting for a real bound.

func TestObserveLocalKill_ReadsLivenessAndSilenceFromInjectedReadings(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	now := start.Add(91 * time.Second)

	alive := ObserveLocalKill(4242, start, now, func(pid int) bool { return pid == 4242 })
	if !alive.Observed || alive.Pid != 4242 || !alive.Alive || alive.SilentFor != 91*time.Second {
		t.Fatalf("alive reading = %+v, want observed pid=4242 alive silent=91s", alive)
	}

	gone := ObserveLocalKill(4242, start, now, func(int) bool { return false })
	if gone.Alive {
		t.Fatalf("gone reading = %+v, want Alive=false", gone)
	}
	if nilFn := ObserveLocalKill(7, start, now, nil); nilFn.Alive || !nilFn.Observed {
		t.Fatalf("nil liveness reader must report unobserved-alive, got %+v", nilFn)
	}
}

func TestAnnotateNeverHangKillWithEvidence_NamesTheAbsentSignalWhenAlive(t *testing.T) {
	msg := AnnotateNeverHangKillWithEvidence("process terminated by signal (signal: killed)", KillEvidence{
		Bound: 2 * time.Minute, Observed: true, Pid: 4242, Alive: true, SilentFor: 119 * time.Second,
	})
	for _, want := range []string{
		"per-attempt never-hang bound of 2m0s",
		"declare `timeout:`",
		"no progress observed for 1m59s",
		"pid 4242",
		"was still alive",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message does not name %q:\n%s", want, msg)
		}
	}
}

func TestAnnotateNeverHangKillWithEvidence_DistinguishesAnExitedProcess(t *testing.T) {
	msg := AnnotateNeverHangKillWithEvidence("killed", KillEvidence{
		Bound: time.Minute, Observed: true, Pid: 99, Alive: false, SilentFor: 60 * time.Second,
	})
	if !strings.Contains(msg, "had already exited") {
		t.Errorf("an exited process must be named as such (the bound was not what ended it):\n%s", msg)
	}
	if strings.Contains(msg, "was still alive") {
		t.Errorf("an exited process must not be reported as alive:\n%s", msg)
	}
}

func TestAnnotateNeverHangKillWithEvidence_UnobservedKeepsTheBoundOnlyMessage(t *testing.T) {
	bound := 90 * time.Second
	got := AnnotateNeverHangKillWithEvidence("boom", KillEvidence{Bound: bound})
	if got != AnnotateNeverHangKill("boom", bound) {
		t.Fatalf("the unobserved case must be the pre-existing message verbatim:\ngot  %q\nwant %q", got, AnnotateNeverHangKill("boom", bound))
	}
	if strings.Contains(got, "no progress observed") {
		t.Fatalf("an unobserved kill must not claim a progress reading:\n%s", got)
	}
}

// The changed path executed live: a REAL local engine command that outlives its bound.
// The message must name the absent signal and the pid that was alive at the bound, the
// process group must be gone afterwards, and the whole thing must return promptly — the
// seam makes the never-hang provable without waiting for a production bound.
func TestRunEngineCommand_BoundFiresNamesTheAbsentSignalAndLeavesNothingRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	began := time.Now()
	_, err := runEngineCommand(ctx, "sleep", "30")
	elapsed := time.Since(began)
	if err == nil {
		t.Fatal("a command outliving its bound must return an error")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("the bound must fire promptly, took %s", elapsed)
	}
	msg := err.Error()
	for _, want := range []string{"never-hang bound", "no progress observed for", "was still alive"} {
		if !strings.Contains(msg, want) {
			t.Errorf("killed engine command message does not name %q:\n%s", want, msg)
		}
	}
	pid := pidFromMessage(t, msg)
	if pid <= 0 {
		t.Fatalf("the message must name the pid it read:\n%s", msg)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("process group of pid %d survived the bound", pid)
	}
}

// pidFromMessage extracts the pid the annotation named, so the test asserts about the
// process the MESSAGE describes rather than about a number it assumes.
func pidFromMessage(t *testing.T, msg string) int {
	t.Helper()
	i := strings.Index(msg, "(pid ")
	if i < 0 {
		return 0
	}
	rest := msg[i+len("(pid "):]
	j := strings.IndexAny(rest, ") ")
	if j < 0 {
		return 0
	}
	pid := 0
	for _, r := range rest[:j] {
		if r < '0' || r > '9' {
			return 0
		}
		pid = pid*10 + int(r-'0')
	}
	return pid
}
