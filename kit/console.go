// console.go is the TRANSPORT-AGNOSTIC console-wizard driver: the mechanism for
// driving a text-console wizard (an OS installer, a first-boot provisioning flow,
// a firmware setup screen) from screenshots + OCR + keyboard input, with the
// recipe supplied as DATA.
//
// It is shared (R3) because MORE THAN ONE transport drives the same wizard: the
// JetKVM verb drives a real machine's keyboard/video, and the SPICE verb drives a
// VM's console — the SAME recipe must produce the SAME drive on either. So the
// engine lives here (an sdk kit is exactly the mechanism this project uses to
// share code across plugin module boundaries) and each transport implements the
// small ConsoleTransport interface.
//
// The recipe is generic DATA: `steps` are {wait_for → action}, `answers` fill
// `{{name}}` placeholders, and the engine knows nothing about any specific
// installer. One engine, any installer, any transport.

package kit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ConsoleStep is ONE step of a console-wizard recipe: OCR-wait for its `wait_for`
// anchor to appear on screen, then perform ONE input action. `wait_for` MUST be a
// screen-UNIQUE string — a string present on every screen (an OS logo, a window
// title) passes vacuously and desynchronises the whole drive.
type ConsoleStep struct {
	WaitFor     string `json:"wait_for"`
	Action      string `json:"action,omitempty"` // key | type | key-combo ("" = pure wait)
	Key         string `json:"key,omitempty"`
	Combo       string `json:"combo,omitempty"`
	Text        string `json:"text,omitempty"`
	TimeoutSec  int    `json:"timeout_sec,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
	Artifact    string `json:"artifact,omitempty"`
	Description string `json:"description,omitempty"`
}

// ConsoleTransport is the per-transport primitive set the engine drives: capture
// the current screen as PNG, and send keyboard input. Each plugin implements it
// over its own wire (JetKVM HID, SPICE keyboard).
type ConsoleTransport interface {
	// Capture returns the CURRENT screen as PNG bytes.
	Capture(ctx context.Context) ([]byte, error)
	// PressKey presses and releases one named key (Return, Escape, F5, ...).
	PressKey(ctx context.Context, name string) error
	// PressCombo presses a modifier chord (ctrl+c, ctrl+alt+Delete, ...).
	PressCombo(ctx context.Context, combo string) error
	// Type types a string.
	Type(ctx context.Context, text string) error
}

// ConsoleOCR runs OCR over a PNG and returns the text. Both transports pass
// OCRBytes (the one canonical tesseract implementation) so the engine and the
// artifact validator agree on how the screen is read.
type ConsoleOCR func(png []byte) (string, error)

// ConsoleWizard drives a recipe over a transport.
type ConsoleWizard struct {
	Steps        []ConsoleStep
	Answers      map[string]string
	Transport    ConsoleTransport
	OCR          ConsoleOCR
	PollInterval time.Duration // default 3s
	// Logger, when set, receives a line per screen seen / input sent (evidence).
	Logger func(format string, args ...any)
}

// ConsoleDefaultTimeoutSec bounds one step's wait for its anchor.
const ConsoleDefaultTimeoutSec = 120

// consoleDefaultPollInterval is how often the engine re-captures while waiting.
// It is a real CONDITION poll (screen content), not a fixed-sleep workaround.
const consoleDefaultPollInterval = 3 * time.Second

// ConsoleStepPlan is one recipe step after answer substitution — the pure,
// testable unit of the engine.
type ConsoleStepPlan struct {
	WaitFor     string
	Action      string
	Key         string
	Combo       string
	Text        string
	TimeoutSec  int
	Optional    bool
	Artifact    string
	Description string
}

// BuildConsolePlan validates an authored recipe and applies answer substitution.
// It is PURE over (steps, answers): no transport, no clock — so a recipe's shape
// is unit-locked without a device and a malformed recipe fails before any session.
func BuildConsolePlan(steps []ConsoleStep, answers map[string]string) ([]ConsoleStepPlan, error) {
	if len(steps) == 0 {
		return nil, fmt.Errorf("a console recipe requires a non-empty steps list")
	}
	out := make([]ConsoleStepPlan, 0, len(steps))
	for i, s := range steps {
		step := ConsoleStepPlan{
			WaitFor:     SubstituteConsoleAnswers(s.WaitFor, answers),
			Action:      s.Action,
			Key:         s.Key,
			Combo:       s.Combo,
			Text:        SubstituteConsoleAnswers(s.Text, answers),
			TimeoutSec:  s.TimeoutSec,
			Optional:    s.Optional,
			Artifact:    s.Artifact,
			Description: s.Description,
		}
		if strings.TrimSpace(step.WaitFor) == "" {
			return nil, fmt.Errorf("step %d: wait_for is required and must be a screen-unique string", i+1)
		}
		if step.TimeoutSec == 0 {
			step.TimeoutSec = ConsoleDefaultTimeoutSec
		}
		switch step.Action {
		case "", "key", "type", "key-combo":
		default:
			return nil, fmt.Errorf("step %d: action must be key, type or key-combo (got %q)", i+1, step.Action)
		}
		switch step.Action {
		case "key":
			if step.Key == "" {
				return nil, fmt.Errorf("step %d: action key requires key", i+1)
			}
		case "key-combo":
			if step.Combo == "" {
				return nil, fmt.Errorf("step %d: action key-combo requires combo", i+1)
			}
		case "type":
			if step.Text == "" {
				return nil, fmt.Errorf("step %d: action type requires text", i+1)
			}
		}
		out = append(out, step)
	}
	return out, nil
}

// SubstituteConsoleAnswers replaces `{{name}}` placeholders in s with
// answers[name], single-pass and literal. An unknown placeholder is left verbatim
// so a mis-authored recipe is visible in the failing step rather than silently
// typing an empty string.
func SubstituteConsoleAnswers(s string, answers map[string]string) string {
	if len(answers) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	var b strings.Builder
	rest := s
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			b.WriteString(rest)
			break
		}
		closeIdx := strings.Index(rest[open:], "}}")
		if closeIdx < 0 {
			b.WriteString(rest)
			break
		}
		closeIdx += open
		name := strings.TrimSpace(rest[open+2 : closeIdx])
		b.WriteString(rest[:open])
		if v, ok := answers[name]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(rest[open : closeIdx+2]) // leave unknown placeholder visible
		}
		rest = rest[closeIdx+2:]
	}
	return b.String()
}

// Run drives the recipe on the transport, OCR-gating each screen. It returns a
// per-step evidence log on success; on a wait timeout it FAILS naming the step,
// the anchor and what OCR actually read (never a silent pass).
func (w *ConsoleWizard) Run(ctx context.Context) (string, error) {
	plan, err := BuildConsolePlan(w.Steps, w.Answers)
	if err != nil {
		return "", err
	}
	poll := w.PollInterval
	if poll <= 0 {
		poll = consoleDefaultPollInterval
	}
	var b strings.Builder
	for i, step := range plan {
		label := step.Description
		if label == "" {
			label = step.WaitFor
		}
		seen, got, err := w.waitFor(ctx, step.WaitFor, step.TimeoutSec, step.Artifact, poll)
		if err != nil {
			return "", fmt.Errorf("step %d (%s): %w", i+1, label, err)
		}
		if !seen {
			if step.Optional {
				fmt.Fprintf(&b, "step %d: skipped (optional; %q not present)\n", i+1, step.WaitFor)
				if w.Logger != nil {
					w.Logger("step %d: skipped (optional; %q not present)", i+1, step.WaitFor)
				}
				continue
			}
			return "", fmt.Errorf("step %d: timed out after %ds waiting for %q; the screen last read %q",
				i+1, step.TimeoutSec, step.WaitFor, ConsolePreview(got, 300))
		}
		fmt.Fprintf(&b, "step %d: saw %q\n", i+1, step.WaitFor)
		if w.Logger != nil {
			w.Logger("step %d: saw %q", i+1, step.WaitFor)
		}
		if err := w.apply(ctx, step); err != nil {
			return "", fmt.Errorf("step %d (%s): %w", i+1, label, err)
		}
		if step.Action != "" {
			fmt.Fprintf(&b, "  sent %s\n", consoleActionSummary(step))
			if w.Logger != nil {
				w.Logger("  sent %s", consoleActionSummary(step))
			}
		}
	}
	return b.String(), nil
}

// waitFor polls capture+OCR until want appears or the deadline fires.
func (w *ConsoleWizard) waitFor(ctx context.Context, want string, timeoutSec int, artifact string, poll time.Duration) (bool, string, error) {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	var last string
	for {
		seen, got, err := w.screenHas(ctx, want, artifact)
		if err != nil {
			return false, last, err
		}
		if seen {
			return true, got, nil
		}
		last = got
		if time.Now().After(deadline) {
			return false, last, nil
		}
		select {
		case <-ctx.Done():
			return false, last, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// screenHas captures the current screen and reports whether want is present.
// A non-nil error means capture/OCR ITSELF failed — distinct from "text absent",
// so a broken OCR setup is never mis-reported as "the screen did not advance".
func (w *ConsoleWizard) screenHas(ctx context.Context, want string, artifact string) (bool, string, error) {
	png, err := w.Transport.Capture(ctx)
	if err != nil {
		return false, "", err
	}
	if artifact != "" {
		if err := AtomicWriteFile(artifact, png, 0o644); err != nil {
			return false, "", fmt.Errorf("saving step artifact %s: %w", artifact, err)
		}
	}
	ocr := w.OCR
	if ocr == nil {
		ocr = OCRBytes
	}
	got, err := ocr(png)
	if err != nil {
		return false, "", err
	}
	return strings.Contains(strings.ToLower(got), strings.ToLower(want)), got, nil
}

// apply sends one step's input through the transport.
func (w *ConsoleWizard) apply(ctx context.Context, step ConsoleStepPlan) error {
	switch step.Action {
	case "":
		return nil
	case "key":
		return w.Transport.PressKey(ctx, step.Key)
	case "key-combo":
		return w.Transport.PressCombo(ctx, step.Combo)
	case "type":
		return w.Transport.Type(ctx, step.Text)
	}
	return nil
}

// consoleActionSummary renders a step's action for the evidence line.
func consoleActionSummary(s ConsoleStepPlan) string {
	switch s.Action {
	case "key":
		return "key " + s.Key
	case "key-combo":
		return "combo " + s.Combo
	case "type":
		return fmt.Sprintf("type %q", s.Text)
	}
	return "(wait only)"
}

// ConsolePreview clips s for an error message (single line).
func ConsolePreview(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ConsoleOCRPSM is tesseract's sparse-text page-segmentation mode. A framebuffer
// console is scattered short labels, not a page of prose, and the default model
// finds little in it — the same mode the artifact validator uses.
const ConsoleOCRPSM = "11"

// OCRBytes runs tesseract over a PNG held in memory and returns its text. It is
// the ONE OCR implementation both console transports and the artifact validator
// rely on (R3). The bytes are written to a temp file because tesseract reads a
// path.
//
// FAILING TO RUN IS NOT FAILING TO MATCH: a missing engine, or missing language
// data (tesseract writes the complaint to stderr and can still exit 0 with EMPTY
// stdout), returns an error that NAMES the cause — never an empty string a caller
// would read as "the text is absent".
func OCRBytes(png []byte) (string, error) {
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		return "", fmt.Errorf("console OCR needs the `tesseract` engine on the HOST running charly and it is not on PATH: %w", err)
	}
	f, err := os.CreateTemp("", "charly-console-ocr-*.png")
	if err != nil {
		return "", fmt.Errorf("console OCR: temp file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(png); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("console OCR: writing temp capture: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("console OCR: closing temp capture: %w", err)
	}

	var stdout, stderr strings.Builder
	cmd := exec.Command(bin, f.Name(), "stdout", "--psm", ConsoleOCRPSM)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// The language-data complaint comes FIRST and is not conditional on the exit
	// status: some tesseract builds exit 0 while writing the error to stderr.
	if msg := stderr.String(); strings.Contains(msg, "Error opening data file") ||
		strings.Contains(msg, "Failed loading language") {
		return "", fmt.Errorf("console OCR: tesseract has no usable language data (install the eng data pack, e.g. tesseract-data-eng): %s", strings.TrimSpace(msg))
	}
	if runErr != nil {
		return "", fmt.Errorf("console OCR: tesseract failed on the capture: %w (stderr: %s)", runErr, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ConsoleRecipe is the TRANSPORT-NEUTRAL recipe bundle a device/recipe entity
// carries: the named recipes, plus the answer sources that fill their
// `{{placeholders}}`. It is the ONE shape both the JetKVM and SPICE console
// drivers decode, so a single authored recipe drives either transport (R3).
type ConsoleRecipe struct {
	// Recipes — name → ordered recipe. Conventional names: "install" (the OS
	// installer) and "first_boot" (the post-reboot provisioning wizard).
	Recipes map[string][]ConsoleStep `json:"recipes,omitempty"`
	// Steps — a shortcut for recipes.install on a single-recipe holder.
	Steps []ConsoleStep `json:"steps,omitempty"`
	// Answers / AnswerSecrets / AnswersEnv — the three answer sources, merged
	// lowest-to-highest: AnswersEnv (env vars) < AnswerSecrets (credential store)
	// < Answers (authored literals).
	Answers       map[string]string `json:"answers,omitempty"`
	AnswerSecrets map[string]string `json:"answer_secrets,omitempty"`
	AnswersEnv    map[string]string `json:"answers_env,omitempty"`
}

// Select returns the named recipe, with the bare `steps:` shortcut serving as the
// default. An unknown name (when recipes are declared) is a clear error.
func (r ConsoleRecipe) Select(name string) ([]ConsoleStep, error) {
	if name == "" {
		name = "install"
	}
	if len(r.Recipes) > 0 {
		if steps, ok := r.Recipes[name]; ok {
			return steps, nil
		}
		return nil, fmt.Errorf("no recipe named %q (declared: %s)", name, strings.Join(sortedRecipeNames(r.Recipes), ", "))
	}
	if name != "install" {
		return nil, fmt.Errorf("no recipe named %q (only the bare steps: shortcut is declared)", name)
	}
	return r.Steps, nil
}

func sortedRecipeNames(m map[string][]ConsoleStep) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MergeAnswers resolves the three answer sources into one map. `env` reads an
// environment variable name, `secret` reads a credential-store key. Precedence
// (lowest first): answers_env < answer_secrets < answers. A nil/empty reader is
// skipped. An unresolved ENV var or secret is left OUT (so a missing var fails
// visibly as an unsubstituted {{placeholder}} rather than silently empty).
func (r ConsoleRecipe) MergeAnswers(env func(string) string, secret func(string) string) map[string]string {
	out := map[string]string{}
	if env != nil {
		for name, key := range r.AnswersEnv {
			if key == "" {
				continue
			}
			if v := env(key); v != "" {
				out[name] = v
			}
		}
	}
	if secret != nil {
		for name, key := range r.AnswerSecrets {
			if key == "" {
				continue
			}
			if v := secret(key); v != "" {
				out[name] = v
			}
		}
	}
	for name, v := range r.Answers {
		out[name] = v
	}
	return out
}
