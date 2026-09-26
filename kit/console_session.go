// console_session.go is the TRANSPORT-AGNOSTIC terminal-session driver: run one
// or more commands in an ALREADY-OPEN terminal on a remote machine's console,
// read each command's result by OCR, and report it as text.
//
// It is the sibling of console.go's ConsoleWizard (which drives a one-shot
// wizard). A wizard answers fixed prompts; a session REUSES one shell across
// commands — so its completion detection cannot key on a prompt (a prompt string
// is not screen-unique and drifts with the shell's PS1). Instead each command is
// followed by an opaque completion MARKER echoed by the shell itself:
//
//	<command>; echo <MARKER>
//
// The engine OCR-waits for MARKER to appear, which means the shell reached the
// end of the command — a REAL condition, never a fixed sleep — and returns the
// screen text read at that moment. A `sudo` command that prompts for a password
// is handled in the same wait: if the sudo password prompt is seen before the
// marker, the password is typed and the wait resumes.
//
// It is shared (R3) for the same reason ConsoleWizard is: the JetKVM verb and
// the SPICE verb drive the same kind of target console, so one engine must serve
// both. The engine holds NO wire type; each transport implements ConsoleTransport.

package kit

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ConsoleCommand is ONE command to run in a terminal session.
type ConsoleCommand struct {
	// Command is the shell command line to type and submit.
	Command string
	// Sudo runs the command through sudo (prefixed with `sudo `), entering
	// SudoPassword at the prompt when the target asks for it.
	Sudo bool
	// Expect is an optional substring that MUST appear in the OCR-read result;
	// when set and absent the command FAILS naming what was read. It is the
	// assertion half of "read the results via OCR".
	Expect string
	// TimeoutSec bounds the wait for the completion marker (default 120).
	TimeoutSec int
	// Artifact, when set, is a host path the completion frame is written to.
	Artifact string
	// Description is an optional evidence label.
	Description string
}

// ConsoleCommandResult is the OCR-read outcome of one command.
type ConsoleCommandResult struct {
	// Command is the line that was run (marker stripped).
	Command string
	// Output is the OCR text read from the screen when the marker appeared.
	Output string
	// Sudo records whether the command ran through sudo.
	Sudo bool
}

// ConsoleSession runs commands on an open terminal over a ConsoleTransport.
type ConsoleSession struct {
	Transport ConsoleTransport
	// OCR defaults to OCRBytes when nil, so every transport reads the screen the
	// same way as the artifact validator.
	OCR func(png []byte) (string, error)
	// SudoPassword is entered at a sudo password prompt (`[sudo] password for …`).
	SudoPassword string
	// PromptAnchors, when set, makes every command FIRST verify a shell prompt is
	// on screen (case-insensitive substring match) before typing. It is the
	// fast-fail guard against typing into the WRONG screen — a pager (`omarchy`
	// help), a menu, a login prompt — where the command and its marker are
	// swallowed and the wait then burns the full timeout. Without it a command is
	// typed blindly (the previous behavior). Default nil = no preflight.
	PromptAnchors []string
	// PromptTimeoutSec bounds the preflight prompt wait (default 30).
	PromptTimeoutSec int
	// PollInterval is how often the engine re-captures while waiting (default
	// ConsoleSessionDefaultPollInterval). A real condition poll, not a sleep.
	PollInterval time.Duration
	// Logger, when set, receives one line per command (evidence).
	Logger func(format string, args ...any)
	// markerCounter disambiguates completion markers within a session.
	markerCounter int
	// nonce makes this session's markers unique against a stale marker left in
	// the scrollback by a previous run (the stale-echo RCA).
	nonce string
}

// ConsoleSessionDefaultTimeoutSec bounds one command's wait for its marker.
const ConsoleSessionDefaultTimeoutSec = 120

// ConsoleSessionDefaultPromptTimeoutSec bounds the preflight shell-prompt wait
// (how long to poll for a shell prompt before refusing to type).
const ConsoleSessionDefaultPromptTimeoutSec = 30

// ConsoleSessionDefaultPollInterval mirrors the wizard's poll cadence.
const ConsoleSessionDefaultPollInterval = 3 * time.Second

// ConsoleSudoPrompt is the substring a sudo password prompt contains (both the
// classic `[sudo] password for user:` and the bare `Password:`). Matched
// case-insensitively against OCR text.
const ConsoleSudoPrompt = "password"

// ConsoleMarkerPrefix identifies a completion marker on screen. It is chosen to
// be OCR-distinguishable (letters + underscores, no symbols OCR confuses).
//
// The full marker carries a per-SESSION nonce after the prefix (see NextMarker).
// WITHOUT the nonce, a marker from a PREVIOUS run left in the terminal scrollback
// is a line equal to the current marker, so the wait completes on the STALE echo
// before the command runs — the RCA behind a flow matching an old `CHARLY_DONE_1`
// line still on screen. A unique token makes a stale marker unmatchable.
const ConsoleMarkerPrefix = "CHARLY_DONE_"

// BuildCommandLine composes the line typed for a command: the command (prefixed
// with sudo when requested), a terminating `; echo <marker>`, so the shell
// echoes marker only after the command has finished. The prefix is appended with
// `;` so it runs regardless of the command's own exit status.
//
// It is PURE, so the exact line the engine types is unit-locked without a device.
func BuildCommandLine(command string, sudo bool, marker string) string {
	line := strings.TrimSpace(command)
	if sudo {
		line = "sudo " + line
	}
	return fmt.Sprintf("%s; echo %s", line, marker)
}

// consoleHasMarkerLine reports whether ANY line of the OCR text equals the marker
// after trimming.
//
// WHY AN EXACT-LINE MATCH, NOT A SUBSTRING (RCA, the echo trap): the terminal
// ECHOES the typed line, which literally contains `echo <marker>` and therefore
// the marker substring — so a substring test would pass the instant the command
// was submitted, before it ran, certifying completion that never happened. The
// echoed input line is the full command, so its trimmed text does NOT equal the
// marker; only the shell's OWN output of `echo <marker>` produces a line whose
// trimmed text IS exactly the marker. That is the real completion condition.
func consoleHasMarkerLine(text, marker string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}

// NextMarker returns the next completion marker for this session. It carries a
// per-session NONCE (a short random token) so a marker from a previous run left
// in the scrollback cannot satisfy this run's wait — the stale-echo RCA. The
// token is letters+digits only (OCR-safe); the counter disambiguates commands
// within one session.
func (s *ConsoleSession) NextMarker() string {
	s.markerCounter++
	if s.nonce == "" {
		s.nonce = markerNonce()
	}
	return fmt.Sprintf("%s%s_%d_END", ConsoleMarkerPrefix, s.nonce, s.markerCounter)
}

// The Linear Congruential Generator constants used by markerNonce. NAMED, not
// inlined, so the mechanism is self-documenting (R4). These are the classic
// Numerical Recipes 64-bit LCG values.
const (
	lcgMultiplier = 6364136223846793005
	lcgIncrement  = 1442695040888963407
	// lcgNonceLen is the nonce length and lcgShift selects a high, well-mixed
	// slice of the LCG state for a near-uniform 0..25 index.
	lcgNonceLen = 8
	lcgShift    = 33
)

// markerNonce returns a short OCR-safe token (8 lowercase letters) seeding a
// 64-bit LCG from the current time. A time-seeded LCG is ample here: the token
// only needs to differ from a marker left by a PREVIOUS run on the SAME screen,
// and this avoids adding a crypto/rand dependency to the SDK for a non-secret
// value. Collisions within one screen are effectively impossible (26^8).
func markerNonce() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, lcgNonceLen)
	seed := uint64(time.Now().UnixNano())
	for i := range b {
		seed = seed*lcgMultiplier + lcgIncrement
		b[i] = alphabet[(seed>>lcgShift)%26]
	}
	return string(b)
}

// RunCommand types one command, submits it, waits for its completion marker
// (entering the sudo password if the target prompts), and returns the OCR-read
// screen text. It is the ONE place a command is driven, so a session of N
// commands is N calls to it — never a second drive loop (R3).
func (s *ConsoleSession) RunCommand(ctx context.Context, c ConsoleCommand) (ConsoleCommandResult, error) {
	cmd := strings.TrimSpace(c.Command)
	if cmd == "" {
		return ConsoleCommandResult{}, fmt.Errorf("console session: command is required")
	}
	if c.Sudo && s.SudoPassword == "" {
		return ConsoleCommandResult{}, fmt.Errorf("console session: command %q needs sudo but no sudo password was supplied", cmd)
	}
	timeout := c.TimeoutSec
	if timeout <= 0 {
		timeout = ConsoleSessionDefaultTimeoutSec
	}
	poll := s.PollInterval
	if poll <= 0 {
		poll = ConsoleSessionDefaultPollInterval
	}
	// Preflight: when prompt anchors are configured, refuse to type into a screen
	// that is not a shell. This is the fast-fail guard against a pager/menu/login
	// prompt swallowing the command and marker (which would otherwise burn the
	// whole timeout and report a misleading "marker not seen").
	if len(s.PromptAnchors) > 0 {
		pt := s.PromptTimeoutSec
		if pt <= 0 {
			pt = ConsoleSessionDefaultPromptTimeoutSec
		}
		got, ok, err := s.WaitForAny(ctx, s.PromptAnchors, pt, "")
		if err != nil {
			return ConsoleCommandResult{}, fmt.Errorf("console session: %q: checking for a shell prompt: %w", cmd, err)
		}
		if !ok {
			return ConsoleCommandResult{Command: cmd, Output: got, Sudo: c.Sudo},
				fmt.Errorf("console session: %q: no shell prompt (%v) is on screen — refusing to type into %q (a pager, menu, or login screen would swallow the command)",
					cmd, s.PromptAnchors, ConsolePreview(got, 200))
		}
	}

	marker := s.NextMarker()

	// Submit `command; echo MARKER`.
	if err := s.Transport.Type(ctx, BuildCommandLine(cmd, c.Sudo, marker)); err != nil {
		return ConsoleCommandResult{}, fmt.Errorf("console session: typing %q: %w", cmd, err)
	}
	if err := s.Transport.PressKey(ctx, "Return"); err != nil {
		return ConsoleCommandResult{}, fmt.Errorf("console session: submitting %q: %w", cmd, err)
	}
	s.logf("command %q submitted (marker %s)", cmd, marker)

	// Wait for the marker LINE, entering the sudo password once if prompted.
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	passwordSent := false
	var last string
	for {
		_, got, err := s.screenHasAny(ctx, nil, c.Artifact)
		if err != nil {
			return ConsoleCommandResult{}, err
		}
		last = got
		if consoleHasMarkerLine(got, marker) {
			output := stripCommandEcho(got, marker)
			res := ConsoleCommandResult{Command: cmd, Output: output, Sudo: c.Sudo}
			if c.Expect != "" && !strings.Contains(strings.ToLower(output), strings.ToLower(c.Expect)) {
				return res, fmt.Errorf("console session: %q: the output does not contain expected %q (read: %q)",
					cmd, c.Expect, ConsolePreview(output, 300))
			}
			s.logf("command %q completed", cmd)
			return res, nil
		}
		if c.Sudo && !passwordSent && strings.Contains(strings.ToLower(got), ConsoleSudoPrompt) {
			if err := s.Transport.Type(ctx, s.SudoPassword); err != nil {
				return ConsoleCommandResult{}, fmt.Errorf("console session: typing sudo password: %w", err)
			}
			if err := s.Transport.PressKey(ctx, "Return"); err != nil {
				return ConsoleCommandResult{}, fmt.Errorf("console session: submitting sudo password: %w", err)
			}
			passwordSent = true
			s.logf("command %q: sudo password entered", cmd)
		}
		if time.Now().After(deadline) {
			return ConsoleCommandResult{Command: cmd, Output: last, Sudo: c.Sudo},
				fmt.Errorf("console session: %q: timed out after %ds waiting for the completion marker; the screen last read %q",
					cmd, timeout, ConsolePreview(last, 300))
		}
		select {
		case <-ctx.Done():
			return ConsoleCommandResult{}, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// stripCommandEcho removes the line the terminal echoed for the submitted command
// from the OCR text, so Output is the command's real output — NOT the echoed input,
// which contains the command text and would otherwise satisfy a caller's `Expect`
// even when the real output never did (the echo trap, in its `Expect` form).
//
// KEYED ON THE MARKER, NOT THE COMMAND (RCA): a real shell echoes the command
// BEHIND its prompt (`root@archiso ~ # cat /etc/os-release | grep ID=; echo
// CHARLY_DONE_1_END`), and OCR mangles the prompt/middle (`setc/…`), so a
// prefix-on-command test misses the echo. The echoed line ALWAYS ends with the
// `echo <marker>` tail — and the marker (an opaque `CHARLY_DONE_N_END` token) is
// also the ONLY line the shell's own output produces for the marker — so we drop
// every line CONTAINING the marker token, including the `echo <marker>` tail
// embedded in the echoed input. No marker-bearing line survives into Output; a
// line without the marker (real output) is kept.
func stripCommandEcho(text, marker string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, marker) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// RunCommands runs each command in order, returning all results. It stops at the
// first failure and returns the results collected so far alongside the error.
func (s *ConsoleSession) RunCommands(ctx context.Context, cmds []ConsoleCommand) ([]ConsoleCommandResult, error) {
	out := make([]ConsoleCommandResult, 0, len(cmds))
	for _, c := range cmds {
		res, err := s.RunCommand(ctx, c)
		out = append(out, res)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// WaitForAny OCR-polls until ANY of the wanted substrings appears, returning the
// full OCR text read. It is the readiness primitive for opening a terminal (the
// shell is ready when its prompt appears) and for passphrase flows where the
// shell is not reachable to echo a marker.
func (s *ConsoleSession) WaitForAny(ctx context.Context, wants []string, timeoutSec int, artifact string) (string, bool, error) {
	if len(wants) == 0 {
		return "", false, fmt.Errorf("console session: WaitForAny needs at least one anchor")
	}
	if timeoutSec <= 0 {
		timeoutSec = ConsoleSessionDefaultTimeoutSec
	}
	poll := s.PollInterval
	if poll <= 0 {
		poll = ConsoleSessionDefaultPollInterval
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	var last string
	for {
		match, got, err := s.screenHasAny(ctx, wants, artifact)
		if err != nil {
			return last, false, err
		}
		last = got
		if match {
			return got, true, nil
		}
		if time.Now().After(deadline) {
			return last, false, nil
		}
		select {
		case <-ctx.Done():
			return last, false, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// EnterPassphrase types a passphrase and submits it with Return, then waits for a
// SUCCESS anchor, failing FAST on a FAILURE anchor (a wrong-passphrase message)
// or on timeout. It is the LUKS/initramfs primitive: no shell exists there to
// echo a marker, so completion is a screen condition the caller names.
//
// ok=true means a success anchor appeared; a failure anchor returns an error
// naming it (never a silent success — a wrong passphrase is a real failure, not
// an "outcome"). The OCR text read at the decision is returned either way.
func (s *ConsoleSession) EnterPassphrase(ctx context.Context, passphrase string, successAnchors, failureAnchors []string, timeoutSec int, artifact string) (string, bool, error) {
	if passphrase == "" {
		return "", false, fmt.Errorf("console session: passphrase is required")
	}
	if len(successAnchors) == 0 {
		return "", false, fmt.Errorf("console session: EnterPassphrase needs at least one success anchor")
	}
	if err := s.Transport.Type(ctx, passphrase); err != nil {
		return "", false, fmt.Errorf("console session: typing passphrase: %w", err)
	}
	if err := s.Transport.PressKey(ctx, "Return"); err != nil {
		return "", false, fmt.Errorf("console session: submitting passphrase: %w", err)
	}
	s.logf("passphrase submitted; awaiting success %v (fail fast on %v)", successAnchors, failureAnchors)
	if timeoutSec <= 0 {
		timeoutSec = ConsoleSessionDefaultTimeoutSec
	}
	poll := s.PollInterval
	if poll <= 0 {
		poll = ConsoleSessionDefaultPollInterval
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	var last string
	for {
		found, which, got, err := s.screenHasSet(ctx, successAnchors, failureAnchors, artifact)
		if err != nil {
			return last, false, err
		}
		last = got
		if found {
			if which == "failure" {
				return got, false, fmt.Errorf("console session: the passphrase was rejected — the screen shows %q", ConsolePreview(got, 200))
			}
			return got, true, nil
		}
		if time.Now().After(deadline) {
			return last, false, nil
		}
		select {
		case <-ctx.Done():
			return last, false, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// screenHasSet captures once and reports whether a SUCCESS or FAILURE anchor is
// present, returning which set matched and the full OCR text. A capture/OCR error
// is distinct from "absent".
func (s *ConsoleSession) screenHasSet(ctx context.Context, success, failure []string, artifact string) (found bool, which, text string, err error) {
	png, err := s.Transport.Capture(ctx)
	if err != nil {
		return false, "", "", err
	}
	if artifact != "" {
		if err := AtomicWriteFile(artifact, png, 0o644); err != nil {
			return false, "", "", fmt.Errorf("saving passphrase artifact %s: %w", artifact, err)
		}
	}
	ocr := s.OCR
	if ocr == nil {
		ocr = OCRBytes
	}
	got, err := ocr(png)
	if err != nil {
		return false, "", "", err
	}
	lower := strings.ToLower(got)
	for _, w := range failure {
		if w != "" && strings.Contains(lower, strings.ToLower(w)) {
			return true, "failure", got, nil
		}
	}
	for _, w := range success {
		if w != "" && strings.Contains(lower, strings.ToLower(w)) {
			return true, "success", got, nil
		}
	}
	return false, "", got, nil
}

// screenHasAny captures once and reports whether ANY wanted substring is present,
// returning the full OCR text. A capture/OCR error is distinct from "absent".
func (s *ConsoleSession) screenHasAny(ctx context.Context, wants []string, artifact string) (bool, string, error) {
	png, err := s.Transport.Capture(ctx)
	if err != nil {
		return false, "", err
	}
	if artifact != "" {
		if err := AtomicWriteFile(artifact, png, 0o644); err != nil {
			return false, "", fmt.Errorf("saving command artifact %s: %w", artifact, err)
		}
	}
	ocr := s.OCR
	if ocr == nil {
		ocr = OCRBytes
	}
	got, err := ocr(png)
	if err != nil {
		return false, "", err
	}
	lower := strings.ToLower(got)
	for _, w := range wants {
		if strings.Contains(lower, strings.ToLower(w)) {
			return true, got, nil
		}
	}
	return false, got, nil
}

func (s *ConsoleSession) logf(format string, args ...any) {
	if s.Logger != nil {
		s.Logger(format, args...)
	}
}
