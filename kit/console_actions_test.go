package kit

// console_actions_test.go pins the shared console-action layer's PURE surface:
// the required-field guards, the defaults, and the evidence rendering. The
// composition itself is a thin call over ConsoleSession/ConsoleFlow, whose
// behavior is tested in console_session_test.go / console_flow_test.go, and is
// exercised end to end LIVE by the jetkvm beds (run-command read `ID=omarchy`
// from a real shell; flow logged in and ran efibootmgr). Re-driving it here
// through a fake OCR would duplicate the engine tests (R3), so this file does
// not.

import (
	"context"
	"strings"
	"testing"
)

// TestRunCommands_SharedEmptyFails guards the required list before any session.
func TestRunCommands_SharedEmptyFails(t *testing.T) {
	if _, err := RunCommands(context.Background(), nil, nil, "", false, nil); err == nil {
		t.Fatal("empty commands must fail")
	}
}

// TestLUKSUnlock_SharedRequiresPassphrase pins the guard (checked before any
// transport call, so a nil transport is safe here).
func TestLUKSUnlock_SharedRequiresPassphrase(t *testing.T) {
	if _, err := LUKSUnlock(context.Background(), nil, "", nil, nil, 1, ""); err == nil {
		t.Fatal("empty passphrase must fail")
	}
}

// TestBootOrder_SharedGuards pins every action guard in the shared layer.
func TestBootOrder_SharedGuards(t *testing.T) {
	cases := []struct{ name, action, entry, seq, want string }{
		{"no action", "", "", "", "requires an action"},
		{"next no entry", "next", "", "", "requires an entry"},
		{"set no seq", "set", "", "", "requires a sequence"},
		{"bad action", "frob", "", "", "is not list | next | set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RunBootOrder(context.Background(), nil, BootOrder{Action: tc.action, Entry: tc.entry, Sequence: tc.seq})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// TestBootOrder_SharedNextNeedsSudo pins the NVRAM-write privilege guard.
func TestBootOrder_SharedNextNeedsSudo(t *testing.T) {
	if _, err := RunBootOrder(context.Background(), nil, BootOrder{Action: "next", Entry: "0003"}); err == nil || !strings.Contains(err.Error(), "needs sudo") {
		t.Fatalf("want sudo guard, got %v", err)
	}
}

// TestRenderFlowEvidence pins that a command node's OCR output is included, so
// "read the results via OCR" is visible in the verdict.
func TestRenderFlowEvidence(t *testing.T) {
	res := ConsoleFlowResult{
		LogText: "step 1: a -> b\n",
		Steps:   []ConsoleStepResult{{Node: "a", Outcome: "b", CommandOutput: "REAL-OUTPUT"}},
	}
	got := RenderFlowEvidence(res)
	if !strings.Contains(got, "step 1: a -> b") || !strings.Contains(got, "REAL-OUTPUT") {
		t.Fatalf("evidence incomplete: %q", got)
	}
}

// TestActionDefaults pins the documented defaults.
func TestActionDefaults(t *testing.T) {
	if DefaultTerminalCombo != "super+Return" {
		t.Fatalf("terminal combo default changed: %q", DefaultTerminalCombo)
	}
	if len(DefaultPromptAnchors) == 0 || len(DefaultLUKSSuccessAnchors) == 0 || len(DefaultLUKSFailureAnchors) == 0 {
		t.Fatal("prompt/LUKS defaults must be non-empty")
	}
	if DefaultBootManager != "efibootmgr" {
		t.Fatalf("boot manager default changed: %q", DefaultBootManager)
	}
}
