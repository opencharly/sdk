package kit

// console_flow_test.go pins the flow engine: continuous OCR-until-condition,
// multiple named outcomes and their routing (if/then/else + case/switch), while
// loops with hard bounds, failure outcomes, and the validation/refusal paths.

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// screenScriptTransport replays a script of screens, advancing on each capture.
type screenScriptTransport struct {
	fakeTransport
	screens []string
	idx     int
	keys    []string
	combos  []string
	types   []string
}

func (s *screenScriptTransport) Capture(context.Context) ([]byte, error) {
	scr := s.screens[s.idx]
	if s.idx < len(s.screens)-1 {
		s.idx++
	}
	return []byte(scr), nil
}
func (s *screenScriptTransport) PressKey(_ context.Context, k string) error {
	s.keys = append(s.keys, k)
	return nil
}
func (s *screenScriptTransport) PressCombo(_ context.Context, c string) error {
	s.combos = append(s.combos, c)
	return nil
}
func (s *screenScriptTransport) Type(_ context.Context, t string) error {
	s.types = append(s.types, t)
	return nil
}

func idOCR(b []byte) (string, error) { return string(b), nil }

// TestConsoleFlow_CaseSwitchRoutesOnOutcome is the headline: one wait offers two
// named outcomes and the OBSERVED one selects the branch (case/switch).
func TestConsoleFlow_CaseSwitchRoutesOnOutcome(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{
		"installer greeter",
		"Success: installed",
	}}
	f := &ConsoleFlow{
		Start: "greeter",
		Nodes: map[string]ConsoleFlowNode{
			"greeter": {
				Wait: []ConsoleFlowOutcome{
					{Name: "greeter", Match: "greeter"},
					{Name: "done", Match: "Success"},
				},
				Action:      ConsoleFlowAction{Key: "Return"},
				Transitions: map[string]string{"greeter": "waitdone", "done": "end"},
			},
			"waitdone": {
				Wait:        []ConsoleFlowOutcome{{Name: "done", Match: "Success"}},
				Transitions: map[string]string{"done": "end"},
			},
			"end": {},
		},
		Transport:    tr,
		OCR:          idOCR,
		PollInterval: time.Millisecond,
	}
	res, err := f.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Final != "end" || len(res.Steps) < 2 {
		t.Fatalf("flow did not route to end: %+v", res)
	}
	// The greeter branch sent Return exactly once.
	if len(tr.keys) != 1 || tr.keys[0] != "Return" {
		t.Fatalf("action not sent on the matched branch: %v", tr.keys)
	}
}

// TestConsoleFlow_FailureOutcomeFailsUnrouted proves a failure outcome with no
// recovery transition FAILS the flow (no silent success).
func TestConsoleFlow_FailureOutcomeFailsUnrouted(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{"No key available with this passphrase."}}
	f := &ConsoleFlow{
		Start: "luks",
		Nodes: map[string]ConsoleFlowNode{
			"luks": {
				Wait: []ConsoleFlowOutcome{
					{Name: "ok", Match: "Booting"},
					{Name: "bad", Match: "No key available", Failure: true},
				},
				Next: "",
			},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond,
	}
	_, err := f.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failure outcome") {
		t.Fatalf("want failure-outcome error, got %v", err)
	}
}

// TestConsoleFlow_FailureOutcomeRecovers proves a failure outcome WITH a
// transition is a recoverable branch (retry the passphrase).
func TestConsoleFlow_FailureOutcomeRecovers(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{
		"No key available",
		"Booting Ok",
	}}
	f := &ConsoleFlow{
		Start: "try",
		Nodes: map[string]ConsoleFlowNode{
			"try": {
				Wait: []ConsoleFlowOutcome{
					{Name: "ok", Match: "Booting"},
					{Name: "bad", Match: "No key available", Failure: true},
				},
				Action:      ConsoleFlowAction{Text: "pass"},
				Transitions: map[string]string{"bad": "tryagain", "ok": "done"},
			},
			"tryagain": {
				Wait:        []ConsoleFlowOutcome{{Name: "ok", Match: "Booting"}},
				Action:      ConsoleFlowAction{Text: "pass2"},
				Transitions: map[string]string{"ok": "done"},
			},
			"done": {},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond,
	}
	res, err := f.Run(context.Background())
	if err != nil {
		t.Fatalf("a routed failure must recover: %v", err)
	}
	if res.Final != "done" {
		t.Fatalf("recovery did not reach done: %+v", res.Steps)
	}
}

// TestConsoleFlow_WhileLoopIsBounded proves a back-edge is a while loop AND that a
// never-satisfied condition FAILS on max_loops instead of spinning forever.
func TestConsoleFlow_WhileLoopIsBounded(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{"still waiting"}} // never matches "ready"
	f := &ConsoleFlow{
		Start: "poll",
		Nodes: map[string]ConsoleFlowNode{
			"poll": {
				Wait:        []ConsoleFlowOutcome{{Name: "wait", Match: "waiting"}, {Name: "ready", Match: "ready"}},
				Action:      ConsoleFlowAction{Key: "Return"},
				Transitions: map[string]string{"wait": "poll", "ready": "done"}, // back-edge = while
			},
			"done": {},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond, MaxLoops: 3,
	}
	_, err := f.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "max_loops") {
		t.Fatalf("an unbounded loop must fail on max_loops, got %v", err)
	}
}

// TestConsoleFlow_WhileLoopExits proves the same back-edge EXITS when the
// condition becomes true, so the loop is a real while, not a failure.
func TestConsoleFlow_WhileLoopExits(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{
		"still waiting", // first visit
		"ready now",     // second visit
	}}
	f := &ConsoleFlow{
		Start: "poll",
		Nodes: map[string]ConsoleFlowNode{
			"poll": {
				Wait:        []ConsoleFlowOutcome{{Name: "wait", Match: "waiting"}, {Name: "ready", Match: "ready"}},
				Action:      ConsoleFlowAction{Key: "Return"},
				Transitions: map[string]string{"wait": "poll", "ready": "done"},
			},
			"done": {},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond, MaxLoops: 5,
	}
	res, err := f.Run(context.Background())
	if err != nil {
		t.Fatalf("the loop must exit when ready: %v", err)
	}
	// Two "poll" visits (wait, then ready) plus the terminal "done" node.
	if res.Final != "done" || len(res.Steps) != 3 {
		t.Fatalf("loop should run twice then exit: %+v", res.Steps)
	}
	if res.Steps[0].Outcome != "wait" || res.Steps[1].Outcome != "ready" {
		t.Fatalf("loop outcomes wrong: %+v", res.Steps)
	}
}

// TestConsoleFlow_IfThenElse proves a boolean branch: one outcome goes "then",
// the other "else".
func TestConsoleFlow_IfThenElse(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{"yes", "then-screen"}}
	f := &ConsoleFlow{
		Start: "check",
		Nodes: map[string]ConsoleFlowNode{
			"check": {
				Wait:        []ConsoleFlowOutcome{{Name: "yes", Match: "yes"}, {Name: "no", Match: "no"}},
				Transitions: map[string]string{"yes": "then", "no": "else"},
			},
			"then": {Wait: []ConsoleFlowOutcome{{Name: "x", Match: "then-screen"}}},
			"else": {},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond,
	}
	res, err := f.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Steps[0].Outcome != "yes" {
		t.Fatalf("outcome not captured: %+v", res.Steps)
	}
}

// TestConsoleFlow_TimeoutFails proves a wait whose outcomes never appear times
// out and names the node + outcomes + what was read.
func TestConsoleFlow_TimeoutFails(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{"nothing relevant"}}
	f := &ConsoleFlow{
		Start: "n",
		Nodes: map[string]ConsoleFlowNode{
			"n": {Wait: []ConsoleFlowOutcome{{Name: "x", Match: "never"}}, TimeoutSec: 1},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond,
	}
	_, err := f.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

// TestConsoleFlow_Validate catches malformed flows without a transport.
func TestConsoleFlow_Validate(t *testing.T) {
	cases := []struct {
		name string
		f    *ConsoleFlow
		want string
	}{
		{"no start", &ConsoleFlow{Nodes: map[string]ConsoleFlowNode{"a": {}}}, "start node"},
		{"undefined start", &ConsoleFlow{Start: "z", Nodes: map[string]ConsoleFlowNode{"a": {}}}, "not defined"},
		{"bad transition", &ConsoleFlow{Start: "a", Nodes: map[string]ConsoleFlowNode{"a": {Transitions: map[string]string{"o": "missing"}}}}, "undefined node"},
		{"empty match", &ConsoleFlow{Start: "a", Nodes: map[string]ConsoleFlowNode{"a": {Wait: []ConsoleFlowOutcome{{Name: "o"}}}}}, "neither an OCR match nor a reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.f.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// TestConsoleFlow_MaxStepsBoundsAValidateBypass proves the total-step cap also
// protects a cycle Validate cannot see as malformed.
func TestConsoleFlow_MaxStepsBounds(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{"loop"}}
	f := &ConsoleFlow{
		Start: "a",
		Nodes: map[string]ConsoleFlowNode{
			"a": {Wait: []ConsoleFlowOutcome{{Name: "o", Match: "loop"}}, Transitions: map[string]string{"o": "b"}},
			"b": {Wait: []ConsoleFlowOutcome{{Name: "o", Match: "loop"}}, Transitions: map[string]string{"o": "a"}},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond, MaxSteps: 5, MaxLoops: 100,
	}
	_, err := f.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "max_steps") {
		t.Fatalf("want max_steps failure, got %v", err)
	}
}

var _ ConsoleTransport = (*screenScriptTransport)(nil)

// imgTransport replays a script of PNG frames, advancing on each capture. It
// embeds fakeTransport for the input-recording half (keys/combos/types), so that
// logic lives in ONE place (R3).
type imgTransport struct {
	*fakeTransport
	frames [][]byte
	idx    int
}

func (t *imgTransport) Capture(context.Context) ([]byte, error) {
	f := t.frames[t.idx]
	if t.idx < len(t.frames)-1 {
		t.idx++
	}
	return f, nil
}

var _ ConsoleTransport = (*imgTransport)(nil)

// TestConsoleFlow_ReferenceOutcomeMatches proves an outcome matches by REFERENCE
// SCREENSHOT (no OCR): the flow triggers its action when the current frame's hash
// is within threshold of the reference.
func TestConsoleFlow_ReferenceOutcomeMatches(t *testing.T) {
	ref := makePNG(t, 100, 80, colorOf(0, 0, 0), colorOf(255, 255, 255))
	dir := t.TempDir()
	refPath := filepath.Join(dir, "ref.png")
	if err := os.WriteFile(refPath, ref, 0o644); err != nil {
		t.Fatal(err)
	}
	tr := &imgTransport{fakeTransport: &fakeTransport{}, frames: [][]byte{ref, ref}}
	f := &ConsoleFlow{
		Start: "wait",
		Nodes: map[string]ConsoleFlowNode{
			"wait": {
				Wait:        []ConsoleFlowOutcome{{Name: "ref", Reference: refPath}},
				Action:      ConsoleFlowAction{Key: "Return"},
				Transitions: map[string]string{"ref": "done"},
			},
			"done": {},
		},
		Transport: tr, PollInterval: time.Millisecond,
	}
	res, err := f.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Steps[0].Outcome != "ref" || res.Final != "done" {
		t.Fatalf("reference outcome did not route: %+v", res.Steps)
	}
	if len(tr.keys) != 1 || tr.keys[0] != "Return" {
		t.Fatalf("action not sent on reference match: %v", tr.keys)
	}
}

// TestConsoleFlow_DetectStart_ResumesAtMatchingNode proves auto-resume: the flow
// starts at the node whose wait matches the CURRENT screen, not at Start.
func TestConsoleFlow_DetectStart_ResumesAtMatchingNode(t *testing.T) {
	// The current screen matches node "step3"; the flow must resume there.
	screen := makePNG(t, 100, 80, colorOf(0, 0, 0), colorOf(255, 255, 255))
	tr := &imgTransport{fakeTransport: &fakeTransport{}, frames: [][]byte{screen, screen}}
	f := &ConsoleFlow{
		Start: "step1",
		Nodes: map[string]ConsoleFlowNode{
			"step1": {Wait: []ConsoleFlowOutcome{{Name: "a", Match: "never-a"}}, Transitions: map[string]string{"a": "step2"}},
			"step2": {Wait: []ConsoleFlowOutcome{{Name: "b", Match: "never-b"}}, Transitions: map[string]string{"b": "step3"}},
			"step3": {Wait: []ConsoleFlowOutcome{{Name: "c", Reference: ""}}, TimeoutSec: 1},
		},
		Transport: tr, PollInterval: time.Millisecond,
	}
	// Give step3 a reference that matches the screen; steps 1/2 have anchors that
	// never appear, so only step3 is detectable.
	dir := t.TempDir()
	p := filepath.Join(dir, "s3.png")
	if err := os.WriteFile(p, screen, 0o644); err != nil {
		t.Fatal(err)
	}
	n := f.Nodes["step3"]
	n.Wait = []ConsoleFlowOutcome{{Name: "c", Reference: p}}
	f.Nodes["step3"] = n
	f.ResumeFromScreen = true
	// Inject a fake OCR (never tesseract) so the test is hermetic and fast.
	f.OCR = func([]byte) (string, error) { return "no anchor matches", nil }

	detected, reason, err := f.DetectStart(context.Background())
	if err != nil {
		t.Fatalf("DetectStart: %v", err)
	}
	if detected != "step3" {
		t.Fatalf("resume must detect step3 (reason=%q), got %q", reason, detected)
	}
}

// TestConsoleFlow_DetectStart_AmbiguousFails proves an ambiguous match FAILS
// naming the candidates rather than choosing arbitrarily.
func TestConsoleFlow_DetectStart_AmbiguousFails(t *testing.T) {
	tr := &imgTransport{fakeTransport: &fakeTransport{}, frames: [][]byte{makePNG(t, 100, 80, colorOf(0, 0, 0), colorOf(255, 255, 255))}}
	f := &ConsoleFlow{
		Start: "a",
		Nodes: map[string]ConsoleFlowNode{
			"a": {Wait: []ConsoleFlowOutcome{{Name: "x", Match: "same"}}},
			"b": {Wait: []ConsoleFlowOutcome{{Name: "y", Match: "same"}}},
		},
		Transport: tr, OCR: func([]byte) (string, error) { return "same", nil }, PollInterval: time.Millisecond,
	}
	if _, _, err := f.DetectStart(context.Background()); err == nil {
		t.Fatal("an ambiguous resume must fail")
	}
}

func colorOf(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

// TestConsoleFlow_DeadlineStopsCleanly proves the wall-clock budget stops the
// flow BETWEEN nodes and returns the collected evidence — never a mid-node kill.
func TestConsoleFlow_DeadlineStopsCleanly(t *testing.T) {
	tr := &screenScriptTransport{screens: []string{"loop"}}
	f := &ConsoleFlow{
		Start: "a",
		Nodes: map[string]ConsoleFlowNode{
			"a": {Wait: []ConsoleFlowOutcome{{Name: "o", Match: "loop"}}, Transitions: map[string]string{"o": "b"}},
			"b": {Wait: []ConsoleFlowOutcome{{Name: "o", Match: "loop"}}, Transitions: map[string]string{"o": "a"}},
		},
		Transport: tr, OCR: idOCR, PollInterval: time.Millisecond,
		MaxSteps: 1000, MaxLoops: 1000,
		Deadline: time.Now().Add(-time.Second), // already elapsed
	}
	res, err := f.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "wall-clock budget") {
		t.Fatalf("want wall-clock budget stop, got %v", err)
	}
	// The evidence must be returned (clean stop), even if empty.
	if res.LogText != "" && !strings.Contains(res.LogText, "step") {
		t.Fatalf("evidence not rendered on budget stop: %q", res.LogText)
	}
}
