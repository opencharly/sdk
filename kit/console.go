// console.go is the TRANSPORT-AGNOSTIC console-wizard driving MECHANISM: driving
// a text-console wizard (an OS installer, a first-boot provisioning flow, a
// firmware setup screen) from screenshots + OCR + keyboard input, with the recipe
// supplied as plain Go DATA.
//
// It is shared (R3) because MORE THAN ONE transport drives the same wizard: the
// jetkvm verb drives a real machine's keyboard/video, and the spice verb drives a
// VM's console — the SAME recipe must produce the SAME drive on either. An sdk kit
// is the mechanism this project uses to share code across plugin module
// boundaries, so the engine lives here and each transport implements the small
// ConsoleTransport interface.
//
// NO WIRE TYPE LIVES HERE (SDD): ConsoleStep, the recipe maps and the answer maps
// are engine-internal Go inputs. The AUTHORED shape is CUE-sourced in each
// consuming plugin's own `schema/*.cue` (#JetkvmInput / #JetkvmInstallStep,
// #SpiceInput / #SpiceConsoleStep); each plugin decodes its generated params and
// converts them to this neutral form. So the schema stays the single source of the
// authored surface, exactly as the kernel/plugin boundary law requires, and this
// kit holds only the mechanism that consumes it.

package kit

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // register the JPEG decoder so a captured JPEG frame can be upscaled
	"image/png"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ConsoleStep is ONE step of a console-wizard recipe: OCR-wait for its WaitFor
// anchor to appear on screen, then perform ONE input action. WaitFor MUST be a
// screen-UNIQUE string — a string present on every screen (an OS logo, a window
// title) passes vacuously and desynchronises the whole drive.
type ConsoleStep struct {
	WaitFor     string
	Action      string // key | type | key-combo ("" = pure wait)
	Key         string
	Combo       string
	Text        string
	TimeoutSec  int
	Optional    bool
	Artifact    string
	Description string
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

// ConsoleWizard drives a recipe over a transport. OCR defaults to OCRBytes when
// nil, so both transports read the screen the same way as the artifact validator.
type ConsoleWizard struct {
	Steps        []ConsoleStep
	Answers      map[string]string
	Transport    ConsoleTransport
	OCR          func(png []byte) (string, error)
	PollInterval time.Duration
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

// SelectRecipe returns the named recipe from a set of named recipes, with the bare
// `steps` list serving as the single default recipe. PURE over plain Go data: the
// caller decodes its OWN CUE-sourced recipe and passes the result here, so this
// engine holds no wire type. An unknown name (when recipes are declared) is a
// clear error, never a silent empty drive.
func SelectRecipe(recipes map[string][]ConsoleStep, steps []ConsoleStep, name string) ([]ConsoleStep, error) {
	if name == "" {
		name = "install"
	}
	if len(recipes) > 0 {
		if r, ok := recipes[name]; ok {
			return r, nil
		}
		return nil, fmt.Errorf("no recipe named %q (declared: %s)", name, strings.Join(sortedRecipeNames(recipes), ", "))
	}
	if name != "install" {
		return nil, fmt.Errorf("no recipe named %q (only the bare steps shortcut is declared)", name)
	}
	return steps, nil
}

func sortedRecipeNames(m map[string][]ConsoleStep) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MergeAnswers resolves the three answer sources into one map. Precedence
// (lowest first): env < secret < authored. `envName` reads an ENVIRONMENT VARIABLE
// name; `secretName` reads a credential-store key. An unresolved env var or secret
// is left OUT (so a missing var fails visibly as an unsubstituted {{placeholder}}
// rather than silently empty). PURE over its inputs — the caller owns the readers.
func MergeAnswers(envVars, secrets, authored map[string]string, envName, secretName func(string) string) map[string]string {
	out := map[string]string{}
	for name, key := range envVars {
		if key == "" || envName == nil {
			continue
		}
		if v := envName(key); v != "" {
			out[name] = v
		}
	}
	for name, key := range secrets {
		if key == "" || secretName == nil {
			continue
		}
		if v := secretName(key); v != "" {
			out[name] = v
		}
	}
	for name, v := range authored {
		out[name] = v
	}
	return out
}

// Run drives the recipe on the transport, OCR-gating each screen. It returns a
// per-step evidence log on success; on a wait timeout it FAILS naming the step,
// the anchor and what OCR actually read (never a silent pass). An `Optional` step
// whose anchor times out is SKIPPED instead (its action not sent).
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

// ConsolePreview clips s to a single line of at most n characters, for an error
// message. It collapses ALL whitespace runs (newlines included) to single spaces
// and appends an ellipsis when it truncates. It is the ONE such clip in the
// SDK — artifact.go's error paths call it too, so OCR text is rendered the same
// way wherever it is quoted back.
func ConsolePreview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ConsoleOCRPSM is tesseract's sparse-text page-segmentation mode. A framebuffer
// console is scattered short labels, not a page of prose, and the default model
// finds little in it — the same mode the artifact validator uses.
const ConsoleOCRPSM = "11"

// OCRBytes runs tesseract over a PNG held in memory and returns its text. It is
// the ONE OCR implementation in the SDK — the console transports AND the
// artifact validator (RunArtifactValidators' artifact_contains_text) both call
// it, so there is exactly one tesseract invocation to keep correct (R3). The
// bytes are written to a temp file because tesseract reads a path.
//
// FAILING TO RUN IS NOT FAILING TO MATCH: a missing engine, or missing language
// data (tesseract writes the complaint to stderr and can still exit 0 with EMPTY
// stdout), returns an error that NAMES the cause — never an empty string a caller
// would read as "the text is absent".
func OCRBytes(pngBytes []byte) (string, error) {
	return OCRBytesScaled(pngBytes, 1)
}

// OCRBytesScaled is OCRBytes with an explicit nearest-neighbour upscale factor
// applied first (1 = none). The artifact validator upscales a screen-resolution
// capture (see sdk's ocrUpscale); a framebuffer console reads at 1.
func OCRBytesScaled(pngBytes []byte, scale int) (string, error) {
	scaled, err := UpscalePNG(pngBytes, scale)
	if err != nil {
		return "", err
	}
	return ocrPNGFile(scaled)
}

// UpscalePNG returns the PNG enlarged by an integer nearest-neighbour factor
// (scale <= 1 returns the bytes unchanged). Nearest-neighbour keeps glyph edges
// hard, which is what tesseract thresholds against; a smoothing filter blurs thin
// UI type. Exposed because the ENLARGEMENT ITSELF is a behavior worth pinning —
// tesseract read ZERO words from a real 1280x800 desktop capture at 1x, 2x and 3x
// and read it once enlarged, so a test asserts the output geometry.
func UpscalePNG(pngBytes []byte, scale int) ([]byte, error) {
	if scale < 1 {
		scale = 1
	}
	if scale == 1 {
		return pngBytes, nil
	}
	src, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("console OCR: decode capture: %w", err)
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*scale, b.Dy()*scale))
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			dst.Set(x, y, src.At(b.Min.X+x/scale, b.Min.Y+y/scale))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("console OCR: encode upscaled copy: %w", err)
	}
	return buf.Bytes(), nil
}

// ocrPNGFile writes the bytes to a temp PNG and runs tesseract over it.
func ocrPNGFile(pngBytes []byte) (string, error) {
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		return "", fmt.Errorf("OCR needs the `tesseract` engine on the HOST running charly and it is not on PATH: %w", err)
	}
	f, err := os.CreateTemp("", "charly-ocr-*.png")
	if err != nil {
		return "", fmt.Errorf("OCR: temp file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(pngBytes); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("OCR: writing temp capture: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("OCR: closing temp capture: %w", err)
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
		return "", fmt.Errorf("OCR: tesseract has no usable language data (install the eng data pack, e.g. tesseract-data-eng): %s", strings.TrimSpace(msg))
	}
	if runErr != nil {
		return "", fmt.Errorf("OCR: tesseract failed on the capture: %w (stderr: %s)", runErr, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
