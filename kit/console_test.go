package kit

// console_test.go unit-locks the shared console engine's PURE half (recipe
// selection, plan building/validation, answer substitution, the three-source
// answer merge) and the OCR primitive's real execution + error semantics.

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
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

// TestSelectRecipe pins the named/default/unknown paths.
func TestSelectRecipe(t *testing.T) {
	recipes := map[string][]ConsoleStep{
		"install":    {{WaitFor: "Start Install"}},
		"first_boot": {{WaitFor: "Start Setup"}},
	}
	got, err := SelectRecipe(recipes, nil, "first_boot")
	if err != nil || len(got) != 1 || got[0].WaitFor != "Start Setup" {
		t.Fatalf("named select: %v %+v", err, got)
	}
	// Empty name defaults to "install".
	got, err = SelectRecipe(recipes, nil, "")
	if err != nil || got[0].WaitFor != "Start Install" {
		t.Fatalf("default select: %v %+v", err, got)
	}
	// Unknown name errors.
	if _, err := SelectRecipe(recipes, nil, "nope"); err == nil {
		t.Fatal("unknown recipe must error")
	}
	// The bare steps shortcut serves the default recipe only.
	steps := []ConsoleStep{{WaitFor: "only"}}
	if got, err := SelectRecipe(nil, steps, "install"); err != nil || got[0].WaitFor != "only" {
		t.Fatalf("shortcut select: %v %+v", err, got)
	}
	if _, err := SelectRecipe(nil, steps, "first_boot"); err == nil {
		t.Fatal("a non-default name with no recipes must error")
	}
}

// TestMergeAnswers_Precedence is the headline three-source merge: env < secret <
// authored, and an unresolved source is left OUT (not silently empty).
func TestMergeAnswers_Precedence(t *testing.T) {
	envVars := map[string]string{"password": "OMARCHY_PW", "username": "OMARCHY_USER", "email": "OMARCHY_EMAIL"}
	secrets := map[string]string{"username": "OMARCHY_USER_SECRET"}
	authored := map[string]string{"hostname": "a"}
	env := func(k string) string {
		return map[string]string{"OMARCHY_PW": "pwenval", "OMARCHY_USER": "envuser"}[k]
	}
	secret := func(k string) string {
		return map[string]string{"OMARCHY_USER_SECRET": "secretuser"}[k]
	}
	got := MergeAnswers(envVars, secrets, authored, env, secret)
	if got["password"] != "pwenval" {
		t.Fatalf("env answer missing/wrong: %v", got)
	}
	if got["username"] != "secretuser" {
		t.Fatalf("secret must win over env: %v", got)
	}
	if got["hostname"] != "a" {
		t.Fatalf("authored answer missing: %v", got)
	}
	if _, present := got["email"]; present {
		t.Fatalf("an UNRESOLVED env var must be left OUT, not silently empty: %v", got)
	}
}

// TestMergeAnswers_AuthoredWinsOverEverything pins the top of the precedence.
func TestMergeAnswers_AuthoredWinsOverEverything(t *testing.T) {
	got := MergeAnswers(
		map[string]string{"k": "K_ENV"},
		map[string]string{"k": "K_SECRET"},
		map[string]string{"k": "authored"},
		func(string) string { return "env" },
		func(string) string { return "secret" },
	)
	if got["k"] != "authored" {
		t.Fatalf("authored must win: %v", got)
	}
}

// TestMergeAnswers_NilReadersSkipped guards a config with only env sources.
func TestMergeAnswers_NilReadersSkipped(t *testing.T) {
	got := MergeAnswers(map[string]string{"k": "ENV"}, nil, nil, nil, nil)
	if len(got) != 0 {
		t.Fatalf("a nil env reader must skip env sources, got %v", got)
	}
}

// TestOCRBytes_ReadsRenderedText runs the REAL tesseract path over a rendered
// image — OCRBytes executes live end to end (the "executed by no test" block).
// The hand-drawn fixture font is not typographically exact, so the assertion is
// that tesseract RAN and returned text, not an exact glyph match; the exact-match
// contract is covered against a real screen by the live plugin beds. SKIPS
// cleanly when tesseract is absent.
func TestOCRBytes_ReadsRenderedText(t *testing.T) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract not installed")
	}
	pngBytes := renderTextPNG(t, "OMARCH")
	got, err := OCRBytes(pngBytes)
	if err != nil {
		t.Fatalf("OCRBytes: %v", err)
	}
	letters := 0
	for _, r := range got {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			letters++
		}
	}
	if letters < 4 {
		t.Fatalf("tesseract ran but read almost nothing (%q) — the OCR path is not working", got)
	}
}

// TestOCRBytes_MissingBinaryIsNotANoMatch is the "failing to RUN is not failing
// to MATCH" property for the shared OCR primitive.
func TestOCRBytes_MissingBinaryIsNotANoMatch(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no tesseract anywhere on PATH
	pngBytes := renderTextPNG(t, "X")
	_, err := OCRBytes(pngBytes)
	if err == nil {
		t.Fatal("no error with no OCR engine installed")
	}
	if !strings.Contains(err.Error(), "tesseract") {
		t.Fatalf("error must name the missing engine: %v", err)
	}
}

// renderTextPNG draws text with Go's built-in bitmap font at 6x, which
// tesseract reads reliably — the deterministic fixture the OCR tests use.
func renderTextPNG(t *testing.T, text string) []byte {
	t.Helper()
	const scale = 6
	w, h := (len(text)*6+2)*scale, 8*scale
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4+0] = 255
		img.Pix[i*4+1] = 255
		img.Pix[i*4+2] = 255
		img.Pix[i*4+3] = 255
	}
	drawText(img, text, scale, color.RGBA{0, 0, 0, 255})
	f, err := os.CreateTemp(t.TempDir(), "ocr-*.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// drawText renders ASCII text using the 5x7 bitmap font below (only the letters
// the tests use, plus a space). Scale enlarges each pixel into a scale×scale block.
func drawText(img *image.RGBA, text string, scale int, c color.RGBA) {
	glyphs := map[rune][7]string{
		'O': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
		'M': {"10001", "11011", "10101", "10001", "10001", "10001", "10001"},
		'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
		'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
		'C': {"01110", "10001", "10000", "10000", "10000", "10001", "01110"},
		'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
		'Y': {"10001", "10001", "01010", "00100", "00100", "00100", "00100"},
		'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
		' ': {"00000", "00000", "00000", "00000", "00000", "00000", "00000"},
	}
	ox := scale
	for _, r := range strings.ToUpper(text) {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if g[row][col] != '1' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.Set(ox+col*scale+dx, scale+row*scale+dy, c)
					}
				}
			}
		}
		ox += 6 * scale
	}
}

// fakeTransport records the inputs it receives and replays canned screens.
type fakeTransport struct {
	keys   []string
	combos []string
	types  []string
}

func (f *fakeTransport) Capture(context.Context) ([]byte, error) { return nil, nil }
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
// sends the input, using a transport that advances its screen per capture.
func TestConsoleWizard_RunDrivesScreens(t *testing.T) {
	script := []string{
		"greeter: Press Return to Start Install",
		"Username>",
		"Reboot Now",
	}
	ft := &fakeTransport{}
	w := &ConsoleWizard{
		Steps: []ConsoleStep{
			{WaitFor: "Press Return to Start Install", Action: "key", Key: "Return"},
			{WaitFor: "Username>", Action: "type", Text: "aitrawog"},
			{WaitFor: "Reboot Now", Action: "key", Key: "Return"},
		},
		Transport: &scriptedTransport{ft: ft, script: script},
		OCR:       func(png []byte) (string, error) { return string(png), nil },
	}
	out, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "step 3") {
		t.Fatalf("evidence missing steps: %s", out)
	}
	if len(ft.keys) != 2 || len(ft.types) != 1 || ft.types[0] != "aitrawog" {
		t.Fatalf("inputs: keys=%v types=%v", ft.keys, ft.types)
	}
}

// TestConsoleWizard_OptionalStepSkips proves an optional step whose anchor never
// appears is skipped, not failed.
func TestConsoleWizard_OptionalStepSkips(t *testing.T) {
	ft := &fakeTransport{}
	w := &ConsoleWizard{
		Steps: []ConsoleStep{
			{WaitFor: "Never on screen", Action: "key", Key: "Return", Optional: true, TimeoutSec: 1},
			{WaitFor: "Always", Action: "key", Key: "Return", TimeoutSec: 5},
		},
		Transport:    &scriptedTransport{ft: ft, fixed: "Always here"},
		OCR:          func(png []byte) (string, error) { return string(png), nil },
		PollInterval: 1,
	}
	out, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("an optional step must not fail the drive: %v", err)
	}
	if !strings.Contains(out, "skipped (optional") {
		t.Fatalf("evidence should record the skip: %s", out)
	}
	// Only the second step sent input.
	if len(ft.keys) != 1 {
		t.Fatalf("the skipped step must not send input: %v", ft.keys)
	}
}

// scriptedTransport replays a script of screens (advancing on each capture) or a
// fixed screen.
type scriptedTransport struct {
	ft     *fakeTransport
	script []string
	fixed  string
	idx    int
}

func (a *scriptedTransport) Capture(context.Context) ([]byte, error) {
	if a.fixed != "" {
		return []byte(a.fixed), nil
	}
	s := a.script[a.idx]
	if a.idx < len(a.script)-1 {
		a.idx++
	}
	return []byte(s), nil
}
func (a *scriptedTransport) PressKey(_ context.Context, k string) error {
	a.ft.keys = append(a.ft.keys, k)
	return nil
}
func (a *scriptedTransport) PressCombo(_ context.Context, c string) error {
	a.ft.combos = append(a.ft.combos, c)
	return nil
}
func (a *scriptedTransport) Type(_ context.Context, s string) error {
	a.ft.types = append(a.ft.types, s)
	return nil
}

var _ = filepath.Join
