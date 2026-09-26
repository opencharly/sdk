// console_actions.go is the TRANSPORT-AGNOSTIC console-action layer: the
// higher-level verbs a plugin exposes over a console (open a terminal, run a
// command, unlock a LUKS volume, set the boot order, drive a bounded flow) live
// here ONCE, over the neutral ConsoleTransport, so the JetKVM verb and the SPICE
// verb share ONE implementation (R3). A plugin decodes its OWN authored params
// into these neutral inputs and calls the matching function — it owns only the
// CUE-generated decode, never a second drive loop.

package kit

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TerminalOpen describes an `open-terminal` action.
type TerminalOpen struct {
	// Combo is the hotkey that opens a terminal ("super+Return") or a VT switch
	// ("ctrl+alt+F3").
	Combo string
	// PromptAnchors are the substrings that mean a shell prompt is ready.
	PromptAnchors []string
	// TimeoutSec bounds the wait for the prompt.
	TimeoutSec int
	// Artifact, when set, saves the frame captured at the decision.
	Artifact string
}

// DefaultTerminalCombo is used when a TerminalOpen sets no Combo: Omarchy's
// terminal hotkey.
const DefaultTerminalCombo = "super+Return"

// DefaultPromptAnchors are the substrings that mean a shell prompt is ready.
var DefaultPromptAnchors = []string{"$", "#", ">"}

// OpenTerminal opens a terminal on the transport and waits for a shell prompt.
// It is the generic `open-terminal`: the transport sends the combo, the shared
// engine waits for a prompt anchor. One implementation, either transport (R3).
func OpenTerminal(ctx context.Context, tr ConsoleTransport, o TerminalOpen) (string, error) {
	combo := strings.TrimSpace(o.Combo)
	if combo == "" {
		combo = DefaultTerminalCombo
	}
	anchors := o.PromptAnchors
	if len(anchors) == 0 {
		anchors = DefaultPromptAnchors
	}
	s := &ConsoleSession{Transport: tr}
	if err := tr.PressCombo(ctx, combo); err != nil {
		return "", fmt.Errorf("open-terminal: sending %q: %w", combo, err)
	}
	got, ok, err := s.WaitForAny(ctx, anchors, o.TimeoutSec, o.Artifact)
	if err != nil {
		return "", fmt.Errorf("open-terminal: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("open-terminal: no shell prompt (%v) appeared after %q (read %q)",
			anchors, combo, ConsolePreview(got, 200))
	}
	return fmt.Sprintf("Opened a terminal with %q and reached a shell prompt", combo), nil
}

// RunCommands runs ordered commands in an open terminal, reading each result by
// OCR. It is the generic `run-command`; sudoPassword is entered at a sudo prompt.
// closeTerminal sends `exit` afterwards. promptAnchors, when set, make every
// command verify a shell prompt first (the fast-fail guard against typing into a
// pager/menu/login screen). One implementation, either transport (R3).
func RunCommands(ctx context.Context, tr ConsoleTransport, cmds []ConsoleCommand, sudoPassword string, closeTerminal bool, promptAnchors []string) (string, error) {
	if len(cmds) == 0 {
		return "", fmt.Errorf("run-command requires a non-empty commands list")
	}
	s := &ConsoleSession{Transport: tr, SudoPassword: sudoPassword, PromptAnchors: promptAnchors}
	results, runErr := s.RunCommands(ctx, cmds)
	var b strings.Builder
	for _, r := range results {
		label := "command"
		if r.Sudo {
			label = "sudo command"
		}
		fmt.Fprintf(&b, "%s %q ->\n%s\n", label, r.Command, r.Output)
	}
	if closeTerminal {
		if _, err := CloseTerminal(ctx, tr); err != nil {
			return b.String(), fmt.Errorf("run-command: closing terminal: %w", err)
		}
	}
	if runErr != nil {
		return b.String(), fmt.Errorf("run-command: %w", runErr)
	}
	return b.String(), nil
}

// CloseTerminal exits an open terminal. The generic `close-terminal` (R3).
func CloseTerminal(ctx context.Context, tr ConsoleTransport) (string, error) {
	if err := tr.Type(ctx, "exit"); err != nil {
		return "", fmt.Errorf("close-terminal: typing exit: %w", err)
	}
	if err := tr.PressKey(ctx, "Return"); err != nil {
		return "", fmt.Errorf("close-terminal: submitting exit: %w", err)
	}
	return "Sent `exit` to the open terminal", nil
}

// DefaultLUKSSuccessAnchors mean the passphrase was ACCEPTED and boot proceeded.
var DefaultLUKSSuccessAnchors = []string{"login:", "Welcome", "succeeded", "Booting"}

// DefaultLUKSFailureAnchors mean the passphrase was REJECTED.
var DefaultLUKSFailureAnchors = []string{"No key available", "wrong password", "Failed to activate"}

// LUKSUnlock enters a disk-encryption passphrase at the initramfs prompt and
// waits for a success anchor; a failure anchor FAILS fast. It is the generic
// `luks-unlock` (R3).
func LUKSUnlock(ctx context.Context, tr ConsoleTransport, passphrase string, success, failure []string, timeoutSec int, artifact string) (string, error) {
	if strings.TrimSpace(passphrase) == "" {
		return "", fmt.Errorf("luks-unlock requires a passphrase (author `passphrase:`, or `passphrase_secret:` naming a credential-store key)")
	}
	if len(success) == 0 {
		success = DefaultLUKSSuccessAnchors
	}
	if len(failure) == 0 {
		failure = DefaultLUKSFailureAnchors
	}
	s := &ConsoleSession{Transport: tr}
	got, ok, err := s.EnterPassphrase(ctx, passphrase, success, failure, timeoutSec, artifact)
	if err != nil {
		return "", fmt.Errorf("luks-unlock: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("luks-unlock: no success anchor (%v) appeared after entering the passphrase (read %q)",
			success, ConsolePreview(got, 200))
	}
	return fmt.Sprintf("Unlocked the disk with the LUKS passphrase. Read: %s", ConsolePreview(got, 200)), nil
}

// BootOrder describes a generic `boot-order` action — set the UEFI boot order
// from INSIDE the running system via the OS-side EFI boot manager.
type BootOrder struct {
	// Action is list | next | set.
	Action string
	// Entry is the entry number for next.
	Entry string
	// Sequence is the comma-separated order for set.
	Sequence string
	// Binary overrides the boot-manager command (default "efibootmgr").
	Binary string
	// SudoPassword is used for the NVRAM-writing actions (next/set).
	SudoPassword string
}

// DefaultBootManager is the EFI boot-manager binary `boot-order` drives.
const DefaultBootManager = "efibootmgr"

// RunBootOrder sets the UEFI boot order over the terminal. The generic
// `boot-order` (R3). Reading (list) needs no privilege; writing (next/set) does.
func RunBootOrder(ctx context.Context, tr ConsoleTransport, o BootOrder) (string, error) {
	action := strings.TrimSpace(o.Action)
	if action == "" {
		return "", fmt.Errorf("boot-order requires an action (list | next | set)")
	}
	bin := strings.TrimSpace(o.Binary)
	if bin == "" {
		bin = DefaultBootManager
	}
	var cmd string
	needSudo := false
	switch action {
	case "list":
		cmd = bin
	case "next":
		if strings.TrimSpace(o.Entry) == "" {
			return "", fmt.Errorf("boot-order action next requires an entry (the entry number, e.g. 0003)")
		}
		cmd = fmt.Sprintf("%s --bootnext %s", bin, strings.TrimSpace(o.Entry))
		needSudo = true
	case "set":
		if strings.TrimSpace(o.Sequence) == "" {
			return "", fmt.Errorf("boot-order action set requires a sequence (e.g. 0003,0001,0002)")
		}
		cmd = fmt.Sprintf("%s --bootorder %s", bin, strings.TrimSpace(o.Sequence))
		needSudo = true
	default:
		return "", fmt.Errorf("boot-order action %q is not list | next | set", action)
	}
	if needSudo && o.SudoPassword == "" {
		return "", fmt.Errorf("boot-order %s writes the firmware NVRAM and needs sudo; set a sudo password (or run it as root if the shell is already root)", action)
	}
	s := &ConsoleSession{Transport: tr, SudoPassword: o.SudoPassword}
	res, err := s.RunCommand(ctx, ConsoleCommand{Command: cmd, Sudo: needSudo, Expect: "Boot"})
	if err != nil {
		return res.Output, fmt.Errorf("boot-order %s: %w", action, err)
	}
	return fmt.Sprintf("boot-order %s ok:\n%s", action, res.Output), nil
}

// ConsoleFlowSpec is the transport-neutral input to RunConsoleFlow — the fields
// both plugins decode their CUE params into.
type ConsoleFlowSpec struct {
	Start        string
	Nodes        map[string]ConsoleFlowNode
	SudoPassword string
	MaxSteps     int
	MaxLoops     int
	// ResumeFromScreen + ResumeOrder: auto-detect the entry node from the current
	// screen (see ConsoleFlow.ResumeFromScreen / DetectStart).
	ResumeFromScreen bool
	ResumeOrder      []string
	// PromptAnchors guards a node's `command` action against typing into a
	// non-shell screen (a pager/menu/login).
	PromptAnchors []string
	// Deadline bounds the whole flow's wall clock so it returns clean evidence
	// before the host's per-step never-hang kill.
	Deadline time.Time
}

// RunConsoleFlow drives a bounded console flow on the transport. The generic
// `flow` (R3). It returns the flow result on success, or the partial result plus
// the error. One implementation, both transports.
func RunConsoleFlow(ctx context.Context, tr ConsoleTransport, spec ConsoleFlowSpec) (ConsoleFlowResult, error) {
	f := &ConsoleFlow{
		Start:            spec.Start,
		Nodes:            spec.Nodes,
		Transport:        tr,
		SudoPassword:     spec.SudoPassword,
		MaxSteps:         spec.MaxSteps,
		MaxLoops:         spec.MaxLoops,
		ResumeFromScreen: spec.ResumeFromScreen,
		ResumeOrder:      spec.ResumeOrder,
		PromptAnchors:    spec.PromptAnchors,
		Deadline:         spec.Deadline,
	}
	return f.Run(ctx)
}

// RenderFlowEvidence renders a flow run's per-node evidence, including the
// OCR-read output of any command action, so "read the results via OCR" is visible
// in the verdict. Shared so both transports render it identically (R3).
func RenderFlowEvidence(res ConsoleFlowResult) string {
	var b strings.Builder
	b.WriteString(res.LogText)
	for _, s := range res.Steps {
		if s.CommandOutput != "" {
			fmt.Fprintf(&b, "  [%s] command output:\n%s\n", s.Node, s.CommandOutput)
		}
	}
	return b.String()
}
