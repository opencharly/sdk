package kit

// console_session_test.go unit-locks the terminal-session engine's PURE half
// (command-line composition, marker uniqueness) and its LIVE driving half over a
// scripted screen transport — including the sudo-password and passphrase flows —
// so every mode of "run a command, read the result, handle sudo/LUKS" is proven
// without a real device.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestBuildCommandLine pins the exact line typed: the command, optionally
// sudo-prefixed, always terminated by `; echo <marker>` so completion is the
// shell's own echo.
func TestBuildCommandLine(t *testing.T) {
	cases := []struct {
		name    string
		command string
		sudo    bool
		marker  string
		want    string
	}{
		{"plain", "id", false, "CHARLY_DONE_1_END", "id; echo CHARLY_DONE_1_END"},
		{"sudo", "systemctl status sshd", true, "CHARLY_DONE_2_END", "sudo systemctl status sshd; echo CHARLY_DONE_2_END"},
		{"trims", "  uname -r  ", false, "M", "uname -r; echo M"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildCommandLine(tc.command, tc.sudo, tc.marker); got != tc.want {
				t.Errorf("BuildCommandLine = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConsoleSession_NextMarkerUnique pins that each command gets its own marker,
// so a stale marker from a previous command cannot satisfy the next wait.
func TestConsoleSession_NextMarkerUnique(t *testing.T) {
	s := &ConsoleSession{}
	a, b := s.NextMarker(), s.NextMarker()
	if a == b {
		t.Fatalf("markers must be unique: %q == %q", a, b)
	}
	if !strings.Contains(a, ConsoleMarkerPrefix) {
		t.Fatalf("marker %q missing the OCR-safe prefix", a)
	}
}

// TestConsoleSession_RunCommandReadsOutput proves the headline flow: type the
// command + marker, wait for the marker LINE, return the OCR-read output with the
// echoed command stripped, containing the command's real result.
func TestConsoleSession_RunCommandReadsOutput(t *testing.T) {
	ft := &fakeTransport{}
	s := &ConsoleSession{
		Transport: &markerTransport{
			fakeTransport: ft,
			// The screen after submit: the echoed command line, the result, and
			// the shell's own marker line.
			markerOn: "uid=0(root) gid=0(root) groups=0(root)",
		},
		OCR: func(png []byte) (string, error) { return string(png), nil },
	}
	res, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "id", Expect: "uid=0"})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !strings.Contains(res.Output, "uid=0(root)") {
		t.Fatalf("output not read: %q", res.Output)
	}
	if strings.Contains(res.Output, "id; echo") {
		t.Fatalf("the echoed command line must be stripped from output: %q", res.Output)
	}
	if res.Sudo {
		t.Fatal("command was not authored as sudo")
	}
	// The typed text is the command + marker, then Return.
	if len(ft.types) != 1 || !strings.Contains(ft.types[0], "id; echo CHARLY_DONE_") {
		t.Fatalf("typed line wrong: %v", ft.types)
	}
	if len(ft.keys) != 1 || ft.keys[0] != "Return" {
		t.Fatalf("submit key wrong: %v", ft.keys)
	}
}

// TestConsoleHasMarkerLine_NotFooledByEcho is the RCA regression: the terminal
// echoes the typed line (which CONTAINS `echo <marker>`), so a substring test
// would certify completion before the command ran. Only a line whose trimmed
// text IS the marker counts.
func TestConsoleHasMarkerLine_NotFooledByEcho(t *testing.T) {
	marker := "CHARLY_DONE_1_END"
	echoed := "id; echo CHARLY_DONE_1_END"
	if consoleHasMarkerLine(echoed, marker) {
		t.Fatal("the echoed input line must NOT count as completion")
	}
	if !consoleHasMarkerLine("uid=0(root)\nCHARLY_DONE_1_END\n", marker) {
		t.Fatal("the shell's own marker line must count as completion")
	}
}

// TestConsoleSession_RunCommandExpectMismatchFails pins the assertion half: a
// command whose output lacks the expected text FAILS naming what was read.
func TestConsoleSession_RunCommandExpectMismatchFails(t *testing.T) {
	s := &ConsoleSession{
		Transport: &markerTransport{fakeTransport: &fakeTransport{}, markerOn: "error: not found"},
		OCR:       func(png []byte) (string, error) { return string(png), nil },
	}
	_, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "cat /nope", Expect: "hello"})
	if err == nil || !strings.Contains(err.Error(), "does not contain expected") {
		t.Fatalf("want expectation failure, got %v", err)
	}
}

// TestConsoleSession_SudoEntersPasswordOnce proves the sudo flow: the password
// prompt is seen before the marker, the password is typed, and the command then
// completes. It also proves the password is entered EXACTLY once.
func TestConsoleSession_SudoEntersPasswordOnce(t *testing.T) {
	ft := &fakeTransport{}
	mt := &sudoTransport{fakeTransport: ft}
	s := &ConsoleSession{
		Transport:    mt,
		OCR:          func(png []byte) (string, error) { return string(png), nil },
		SudoPassword: "hunter2",
	}
	res, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "id", Sudo: true})
	if err != nil {
		t.Fatalf("sudo RunCommand: %v", err)
	}
	if !res.Sudo {
		t.Fatal("result must record sudo")
	}
	// typed: the sudo command line, then the password; Return after each.
	if len(ft.types) != 2 || !strings.Contains(ft.types[0], "sudo id; echo") {
		t.Fatalf("typed lines wrong: %v", ft.types)
	}
	if ft.types[1] != "hunter2" {
		t.Fatalf("sudo password not typed: %v", ft.types)
	}
	if len(ft.keys) != 2 {
		t.Fatalf("expected Return after command and after password, got %v", ft.keys)
	}
}

// TestConsoleSession_SudoWithoutPasswordFails is the honest refusal: a sudo
// command with no password supplied fails before touching the device.
func TestConsoleSession_SudoWithoutPasswordFails(t *testing.T) {
	s := &ConsoleSession{Transport: &fixedTransport{fakeTransport: &fakeTransport{}, screen: "x"}}
	_, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "id", Sudo: true})
	if err == nil || !strings.Contains(err.Error(), "no sudo password") {
		t.Fatalf("want sudo-password refusal, got %v", err)
	}
}

// TestConsoleSession_EmptyCommandFails guards the required field.
func TestConsoleSession_EmptyCommandFails(t *testing.T) {
	s := &ConsoleSession{Transport: &fixedTransport{fakeTransport: &fakeTransport{}, screen: "x"}}
	if _, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "  "}); err == nil {
		t.Fatal("empty command must fail")
	}
}

// TestConsoleSession_EnterPassphrase proves the LUKS flow: type the passphrase,
// submit, wait for a named outcome anchor.
func TestConsoleSession_EnterPassphrase(t *testing.T) {
	ft := &fakeTransport{}
	s := &ConsoleSession{
		Transport: &fixedTransport{fakeTransport: ft, screen: "Booting Omarchy ..."},
		OCR:       func(png []byte) (string, error) { return string(png), nil },
	}
	got, ok, err := s.EnterPassphrase(context.Background(),
		"disk-pass", []string{"login:", "Booting"}, []string{"wrong password"}, 5, "")
	if err != nil || !ok {
		t.Fatalf("EnterPassphrase: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(got, "Booting") {
		t.Fatalf("outcome text not returned: %q", got)
	}
	if len(ft.types) != 1 || ft.types[0] != "disk-pass" {
		t.Fatalf("passphrase not typed: %v", ft.types)
	}
	if len(ft.keys) != 1 || ft.keys[0] != "Return" {
		t.Fatalf("passphrase not submitted: %v", ft.keys)
	}
}

// TestConsoleSession_EnterPassphraseEmptyFails guards the required field.
func TestConsoleSession_EnterPassphraseEmptyFails(t *testing.T) {
	s := &ConsoleSession{Transport: &fixedTransport{fakeTransport: &fakeTransport{}, screen: "x"}}
	if _, _, err := s.EnterPassphrase(context.Background(), "", []string{"x"}, nil, 1, ""); err == nil {
		t.Fatal("empty passphrase must fail")
	}
}

// TestConsoleSession_EnterPassphraseWrongFailsFast is the critical semantic: a
// wrong-passphrase anchor FAILS the flow (never a silent "outcome"), with the
// message naming what the screen showed.
func TestConsoleSession_EnterPassphraseWrongFailsFast(t *testing.T) {
	s := &ConsoleSession{
		Transport: &fixedTransport{fakeTransport: &fakeTransport{}, screen: "No key available with this passphrase."},
		OCR:       func(png []byte) (string, error) { return string(png), nil },
	}
	_, ok, err := s.EnterPassphrase(context.Background(),
		"bad", []string{"login:", "Booting"}, []string{"No key available"}, 5, "")
	if err == nil || ok {
		t.Fatalf("a wrong passphrase must FAIL, got ok=%v err=%v", ok, err)
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("error must say the passphrase was rejected: %v", err)
	}
}

// TestConsoleSession_WaitForAnyTimesOut proves a wait with no matching screen
// returns ok=false at the deadline (a real condition poll), never a false pass.
func TestConsoleSession_WaitForAnyTimesOut(t *testing.T) {
	s := &ConsoleSession{
		Transport:    &fixedTransport{fakeTransport: &fakeTransport{}, screen: "nothing here"},
		OCR:          func(png []byte) (string, error) { return string(png), nil },
		PollInterval: time.Millisecond,
	}
	_, ok, err := s.WaitForAny(context.Background(), []string{"never"}, 1, "")
	if err != nil {
		t.Fatalf("WaitForAny: %v", err)
	}
	if ok {
		t.Fatal("a never-present anchor must time out, not pass")
	}
}

// TestConsoleSession_RunCommandsStopsOnFirstFailure proves a session halts at the
// first failing command and returns the partial results.
func TestConsoleSession_RunCommandsStopsOnFirstFailure(t *testing.T) {
	ft := &fakeTransport{}
	s := &ConsoleSession{
		Transport: &markerTransport{fakeTransport: ft, markerOn: "output"},
		OCR:       func(png []byte) (string, error) { return string(png), nil },
	}
	results, err := s.RunCommands(context.Background(), []ConsoleCommand{
		{Command: "true", Expect: "output"},
		{Command: "false", Expect: "absent-text"},
		{Command: "never-run"},
	})
	if err == nil {
		t.Fatal("a failing command must stop the session")
	}
	if len(results) != 2 {
		t.Fatalf("partial results must be returned: %d", len(results))
	}
}

// markerTransport simulates a shell: it echoes the last typed line, then (once a
// command line — not a bare password — was submitted) prints the result and the
// shell's own marker LINE extracted from the typed `echo <marker>` tail.
type markerTransport struct {
	*fakeTransport
	markerOn string
}

func (m *markerTransport) Capture(context.Context) ([]byte, error) {
	if len(m.types) == 0 {
		return []byte("prompt $ "), nil
	}
	typed := m.types[len(m.types)-1]
	// The first typed line is the command; later types are the sudo password.
	if !strings.Contains(typed, "echo CHARLY_DONE_") {
		// A password was typed: show the sudo result and the previous marker.
		marker := markerFromLine(m.types[0])
		return []byte("uid=0(root) gid=0(root)\n" + marker + "\n"), nil
	}
	// Echo the command BEHIND A PROMPT (as a real shell does), then its output
	// and the shell's own marker line. The prompt prefix is what the old
	// command-prefix stripper missed (the echo trap in its Expect form).
	return []byte("user@host ~ $ " + typed + "\n" + m.markerOn + "\n" + markerFromLine(typed) + "\n"), nil
}

// markerFromLine extracts the marker token from a typed `cmd; echo <marker>` line.
// It uses the LAST `echo ` so a command that itself contains `echo` (e.g.
// `echo PROMPT_OK; echo MARKER`) still yields the marker.
func markerFromLine(line string) string {
	if i := strings.LastIndex(line, "echo "); i >= 0 {
		return strings.TrimSpace(line[i+len("echo "):])
	}
	return ""
}

// sudoTransport simulates a sudo prompt: the first capture after the command is
// submitted shows the password prompt; once the password was typed, the result
// and the marker line are shown.
type sudoTransport struct {
	*fakeTransport
}

func (s *sudoTransport) Capture(context.Context) ([]byte, error) {
	// The sudo password is the SECOND typed line (after the command line).
	if len(s.types) < 2 {
		return []byte("[sudo] password for omarchy: "), nil
	}
	return []byte("uid=0(root) gid=0(root)\n" + markerFromLine(s.types[0]) + "\n"), nil
}

// fixedTransport always returns one screen.
type fixedTransport struct {
	*fakeTransport
	screen string
}

func (f *fixedTransport) Capture(context.Context) ([]byte, error) { return []byte(f.screen), nil }

var (
	_ ConsoleTransport = (*markerTransport)(nil)
	_ ConsoleTransport = (*sudoTransport)(nil)
	_ ConsoleTransport = (*fixedTransport)(nil)
)

// TestConsoleSession_PromptPreflightRefusesNonShell is the RCA fix: with prompt
// anchors set, a command is NOT typed into a screen that is not a shell (a pager,
// a menu) — it fails FAST naming what was on screen, instead of burning the
// marker timeout after swallowing the command.
func TestConsoleSession_PromptPreflightRefusesNonShell(t *testing.T) {
	ft := &fakeTransport{}
	s := &ConsoleSession{
		Transport:        &fixedTransport{fakeTransport: ft, screen: "omarchy help: cmd Command and shortcut helpers"},
		OCR:              func(png []byte) (string, error) { return string(png), nil },
		PromptAnchors:    []string{"~", "#", "$"},
		PromptTimeoutSec: 1,
		PollInterval:     time.Millisecond,
	}
	_, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "echo hi"})
	if err == nil || !strings.Contains(err.Error(), "no shell prompt") {
		t.Fatalf("want prompt-preflight refusal, got %v", err)
	}
	// The command must NOT have been typed.
	if len(ft.types) != 0 {
		t.Fatalf("the command must not be typed into a non-shell screen: %v", ft.types)
	}
}

// TestConsoleSession_PromptPreflightProceedsInShell proves the guard is
// transparent in a real shell: the prompt is present, so the command runs.
func TestConsoleSession_PromptPreflightProceedsInShell(t *testing.T) {
	ft := &fakeTransport{}
	mt := &markerTransport{fakeTransport: ft, markerOn: "PROMPT_OK"}
	s := &ConsoleSession{
		Transport:        mt,
		OCR:              func(png []byte) (string, error) { return string(png), nil },
		PromptAnchors:    []string{"$"},
		PromptTimeoutSec: 1,
		PollInterval:     time.Millisecond,
	}
	// The markerTransport returns "prompt $ " before anything typed, so the
	// preflight sees a prompt and proceeds.
	res, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "echo PROMPT_OK", Expect: "PROMPT_OK"})
	if err != nil {
		t.Fatalf("RunCommand in a shell: %v", err)
	}
	if !strings.Contains(res.Output, "PROMPT_OK") {
		t.Fatalf("output not read: %q", res.Output)
	}
}

// TestConsoleSession_EchoStrippedFromOutput is the validator's RCA regression: a
// real shell echoes the command BEHIND a prompt, and OCR mangles it, so the echo
// must be stripped by its MARKER-bearing line — otherwise the echoed command text
// survives into Output and satisfies a caller's `Expect` even when the real output
// never did (the echo trap in its Expect form: e.g. `Expect: "sshd"` for
// `systemctl status sshd`).
func TestConsoleSession_EchoStrippedFromOutput(t *testing.T) {
	ft := &fakeTransport{}
	s := &ConsoleSession{
		Transport: &markerTransport{fakeTransport: ft, markerOn: "inactive"},
		OCR:       func(png []byte) (string, error) { return string(png), nil },
	}
	res, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "systemctl status sshd"})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	// The echoed command (containing "sshd") must NOT be in Output — only the
	// real result. Otherwise an `Expect: "sshd"` would pass vacuously.
	if strings.Contains(res.Output, "systemctl status sshd") {
		t.Fatalf("the echoed command must be stripped from Output: %q", res.Output)
	}
	if strings.Contains(res.Output, "CHARLY_DONE") {
		t.Fatalf("the marker must not survive into Output: %q", res.Output)
	}
	if !strings.Contains(res.Output, "inactive") {
		t.Fatalf("the real output must be retained: %q", res.Output)
	}
}

// TestConsoleSession_StaleMarkerDoesNotComplete is the RCA regression: a marker
// from a PREVIOUS run left in the scrollback must NOT satisfy this run's wait —
// otherwise the wait completes on the stale echo before the command runs. It
// DRIVES RunCommand over a transport whose screen always shows a stale marker
// line, and asserts the run does NOT complete on it (it times out instead).
func TestConsoleSession_StaleMarkerDoesNotComplete(t *testing.T) {
	// A transport that always shows a stale marker line from an OLD run, plus the
	// echo of the just-typed command (which also carries the CURRENT marker).
	tr := &staleMarkerTransport{fakeTransport: &fakeTransport{}, stale: "CHARLY_DONE_1_END"}
	s := &ConsoleSession{
		Transport:    tr,
		OCR:          func(png []byte) (string, error) { return string(png), nil },
		PollInterval: time.Millisecond,
	}
	_, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "id", TimeoutSec: 1})
	if err == nil {
		t.Fatal("a command must NOT complete on a stale marker line from a previous run")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout (the stale marker is not this run's completion), got %v", err)
	}
}

// TestConsoleSession_MarkerUniquenessAcrossSessions pins the mechanism: two
// sessions' markers differ, markers within one session differ, and a marker is
// OCR-safe.
func TestConsoleSession_MarkerUniquenessAcrossSessions(t *testing.T) {
	a := &ConsoleSession{}
	b := &ConsoleSession{}
	if a.NextMarker() == b.NextMarker() {
		t.Fatalf("markers from two sessions must differ (stale-echo guard): %q", a.NextMarker())
	}
	if a.NextMarker() == a.NextMarker() {
		t.Fatal("markers within a session must differ")
	}
	for _, r := range a.NextMarker() {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			t.Fatalf("marker has a non-OCR-safe char %q", r)
		}
	}
	// The per-session NONCE must be DIGITS ONLY — tesseract confuses similar
	// random letters (b->h, q->g), which would make the marker line never match
	// exactly (the live RCA). The marker shape is CHARLY_DONE_<digits>_<n>_END.
	parts := strings.Split(a.NextMarker(), "_")
	if len(parts) < 4 || parts[2] == "" {
		t.Fatalf("marker shape unexpected: %q", a.NextMarker())
	}
	for _, r := range parts[2] {
		if r < '0' || r > '9' {
			t.Fatalf("the nonce must be digits-only for OCR robustness, got %q in %q", r, parts[2])
		}
	}
}

// staleMarkerTransport always shows a fixed stale marker line (an old run's
// leftover) and echoes the typed line, so a wait keyed on the marker alone would
// complete on the stale line. The current marker (carrying a fresh nonce) never
// appears as an exact line, so the wait must time out.
type staleMarkerTransport struct {
	*fakeTransport
	stale string
}

func (t *staleMarkerTransport) Capture(context.Context) ([]byte, error) {
	line := ""
	if len(t.types) > 0 {
		line = t.types[len(t.types)-1] // the echoed (typed) line
	}
	return []byte("user@host ~ $ " + line + "\nold output\n" + t.stale + "\n"), nil
}

var _ ConsoleTransport = (*staleMarkerTransport)(nil)

// TestConsoleHasMarkerLine_WhitespaceNoise is the live RCA regression: tesseract
// inserts whitespace inside a token (`CHARLY DONE_798525_1_END`, `CHARLY_DONE_
// xpghugbf _1_END`), so a pixel-exact comparison never matches a marker that IS
// on screen. The comparison must normalize whitespace — while STILL being an
// equality test (an echoed command line, which contains the marker plus more,
// must NOT match).
func TestConsoleHasMarkerLine_WhitespaceNoise(t *testing.T) {
	marker := "CHARLY_DONE_79852531_1_END"
	for _, noisy := range []string{
		"CHARLY_DONE_79852531_1_END",
		"CHARLY DONE_79852531_1_END",   // space for underscore
		"CHARLY_DONE_ 79852531_1_END",  // stray space
		"CHARLY_DONE_79852531 _1_END",  // space before the counter
		" CHARLY_DONE_79852531_1_END ", // surrounding whitespace
	} {
		if !consoleHasMarkerLine(noisy, marker) {
			t.Errorf("marker with OCR whitespace noise must match: %q", noisy)
		}
	}
	// The echoed command line contains the marker but is NOT equal to it.
	if consoleHasMarkerLine("user@host ~ $ id; echo CHARLY_DONE_79852531_1_END", marker) {
		t.Fatal("the echoed command line must NOT match (the echo trap)")
	}
}

// TestConsoleSession_SecondCommandIgnoresFirstMarker is the within-session
// regression (from review): the FIRST command's marker is still on screen when
// the SECOND runs. With a counter-only difference and a 2-error tolerance, the
// second wait would match the first marker (`..._1_END` vs `..._2_END` = 1 edit)
// and complete before the second command ran. A FRESH per-command nonce makes
// consecutive markers differ by the whole 12 digits, so this must NOT happen.
func TestConsoleSession_SecondCommandIgnoresFirstMarker(t *testing.T) {
	tr := &twoMarkerTransport{fakeTransport: &fakeTransport{}}
	s := &ConsoleSession{
		Transport:    tr,
		OCR:          func(png []byte) (string, error) { return string(png), nil },
		PollInterval: time.Millisecond,
	}
	// First command completes normally.
	if _, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "echo one", TimeoutSec: 3}); err != nil {
		t.Fatalf("first command: %v", err)
	}
	// Second command: the screen STILL shows the first command's marker line and
	// never shows the second's (the transport stops echoing). It must TIMEOUT, not
	// complete on the first marker.
	tr.freeze = true
	_, err := s.RunCommand(context.Background(), ConsoleCommand{Command: "echo two", TimeoutSec: 1})
	if err == nil {
		t.Fatal("the second command must NOT complete on the FIRST command's marker")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
}

// twoMarkerTransport echoes the first command's marker forever; after freeze it
// keeps showing that first marker and stops emitting new ones.
type twoMarkerTransport struct {
	*fakeTransport
	freeze      bool
	firstMarker string
}

func (t *twoMarkerTransport) Capture(context.Context) ([]byte, error) {
	if t.freeze {
		// Only the FIRST command's REAL marker (captured from its typed line)
		// remains on screen — with a CACHED per-session nonce this is the marker
		// the second wait would collide with.
		return []byte("user@host ~ $ echo one\n" + t.firstMarker + "\n"), nil
	}
	typed := ""
	if len(t.types) > 0 {
		typed = t.types[len(t.types)-1]
	}
	marker := ""
	if i := strings.LastIndex(typed, "echo "); i >= 0 {
		marker = strings.TrimSpace(typed[i+len("echo "):])
		if marker != "" {
			t.firstMarker = marker
		}
	}
	return []byte("user@host ~ $ " + typed + "\n" + marker + "\n"), nil
}

var _ ConsoleTransport = (*twoMarkerTransport)(nil)
