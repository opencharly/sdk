package kit

import (
	"context"
	"strings"
	"testing"
)

func TestBuildConsolePlan_SubstitutesAndValidates(t *testing.T) {
	steps := []ConsoleStep{
		{WaitFor: "Username>", Action: "type", Text: "{{user}}"},
		{WaitFor: "Reboot Now"},
	}
	plan, err := BuildConsolePlan(steps, map[string]string{"user": "aitrawog"})
	if err != nil {
		t.Fatalf("BuildConsolePlan: %v", err)
	}
	if plan[0].Text != "aitrawog" || plan[0].TimeoutSec != ConsoleDefaultTimeoutSec {
		t.Fatalf("plan[0] = %+v", plan[0])
	}
	if plan[1].Action != "" {
		t.Fatalf("action-less step must stay a pure wait: %+v", plan[1])
	}
}

func TestBuildConsolePlan_Rejects(t *testing.T) {
	cases := []struct {
		name  string
		steps []ConsoleStep
		want  string
	}{
		{"empty", nil, "non-empty"},
		{"no wait_for", []ConsoleStep{{Action: "key", Key: "Return"}}, "wait_for is required"},
		{"bad action", []ConsoleStep{{WaitFor: "x", Action: "dance"}}, "action must be"},
		{"key without key", []ConsoleStep{{WaitFor: "x", Action: "key"}}, "requires key"},
		{"combo without combo", []ConsoleStep{{WaitFor: "x", Action: "key-combo"}}, "requires combo"},
		{"type without text", []ConsoleStep{{WaitFor: "x", Action: "type"}}, "requires text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildConsolePlan(tc.steps, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSubstituteConsoleAnswers_LeavesUnknownVisible(t *testing.T) {
	if got := SubstituteConsoleAnswers("{{a}}-{{b}}", map[string]string{"a": "1"}); got != "1-{{b}}" {
		t.Fatalf("got %q", got)
	}
	if got := SubstituteConsoleAnswers("plain", nil); got != "plain" {
		t.Fatalf("identity failed: %q", got)
	}
}

// fakeTransport records the inputs it receives and replays canned screens keyed
// by a "current screen" the test advances.
type fakeTransport struct {
	screen string
	keys   []string
	combos []string
	types  []string
}

func (f *fakeTransport) Capture(context.Context) ([]byte, error) { return []byte(f.screen), nil }
func (f *fakeTransport) PressKey(_ context.Context, k string) error {
	f.keys = append(f.keys, k)
	return nil
}
func (f *fakeTransport) PressCombo(_ context.Context, c string) error {
	f.combos = append(f.combos, c)
	return nil
}
func (f *fakeTransport) Type(_ context.Context, s string) error {
	f.types = append(f.types, s)
	return nil
}

// TestConsoleWizard_RunDrivesScreens proves the engine OCR-gates each step and
// sends the input, using a transport that advances its screen when an input lands.
func TestConsoleWizard_RunDrivesScreens(t *testing.T) {
	ft := &fakeTransport{screen: "greeter: Press Return to Start Install"}
	ocr := func(png []byte) (string, error) { return string(png), nil }
	// A transport whose screen advances per input, mirroring a wizard.
	advance := &advancingTransport{ft: ft, script: []string{
		"greeter: Press Return to Start Install",
		"Username>",
		"Reboot Now",
	}}
	w := &ConsoleWizard{
		Steps: []ConsoleStep{
			{WaitFor: "Press Return to Start Install", Action: "key", Key: "Return"},
			{WaitFor: "Username>", Action: "type", Text: "aitrawog"},
			{WaitFor: "Reboot Now", Action: "key", Key: "Return"},
		},
		Transport: advance, OCR: ocr, PollInterval: 1,
	}
	out, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "step 3") {
		t.Fatalf("evidence missing steps: %s", out)
	}
	if len(advance.ft.keys) != 2 || len(advance.ft.types) != 1 {
		t.Fatalf("inputs: keys=%v types=%v", advance.ft.keys, advance.ft.types)
	}
}

// advancingTransport returns a new screen from a script each time Capture is
// called after an input. It is a test-only convenience.
type advancingTransport struct {
	ft     *fakeTransport
	script []string
	idx    int
}

func (a *advancingTransport) Capture(context.Context) ([]byte, error) {
	s := a.script[a.idx]
	if a.idx < len(a.script)-1 {
		a.idx++
	}
	return []byte(s), nil
}
func (a *advancingTransport) PressKey(_ context.Context, k string) error {
	a.ft.keys = append(a.ft.keys, k)
	return nil
}
func (a *advancingTransport) PressCombo(_ context.Context, c string) error {
	a.ft.combos = append(a.ft.combos, c)
	return nil
}
func (a *advancingTransport) Type(_ context.Context, s string) error {
	a.ft.types = append(a.ft.types, s)
	return nil
}
