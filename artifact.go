package sdk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig / image.Decode
	"image/png"    // ENCODER too: the OCR upscale writes a PNG, so this is a real import
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/opencharly/spec/spec"
)

// ---------------------------------------------------------------------------
// Artifact validators
//
// The post-run artifact-reality assertions (min_bytes / min_dimensions /
// not_uniform / min_cast_events / contains_text) are the SINGLE implementation (R3) every
// out-of-tree verb plugin that produces an artifact (appium screenshot, adb
// screencap, cdp/wl/vnc/record captures) calls — the same property motivating
// MatchAll's home here. Every live-container verb is now served out-of-process,
// so this SDK copy is the sole implementation (the former host duplicate in
// charly's core check runner was deleted with the in-proc live-verb runtime).
// ---------------------------------------------------------------------------

// RunArtifactValidators runs every artifact assertion the step's plugin input
// declares against the file at the input's `artifact` path: artifact_min_bytes,
// artifact_min_dimensions (WxH), artifact_not_uniform, and
// artifact_min_cast_events. The artifact fields live in the desugared plugin
// input map (per-verb fields left core #Op in the schema-compaction cutover).
// Returns nil when every declared validator passes, or the first validator's
// error. A plugin that produces an artifact calls this after writing the file
// as the post-run validation pipeline.
func RunArtifactValidators(op *spec.Op) error {
	artifact := inputString(op, "artifact")
	if n := inputInt(op, "artifact_min_bytes"); n > 0 {
		info, err := os.Stat(artifact)
		if err != nil {
			return fmt.Errorf("artifact %q not found: %w", artifact, err)
		}
		if info.Size() < int64(n) {
			return fmt.Errorf("artifact %q size %d < required min_bytes %d", artifact, info.Size(), n)
		}
	}
	if wxh := inputString(op, "artifact_min_dimensions"); wxh != "" {
		if err := assertArtifactMinDimensions(artifact, wxh); err != nil {
			return err
		}
	}
	if inputBool(op, "artifact_not_uniform") {
		if err := assertArtifactNotUniform(artifact); err != nil {
			return err
		}
	}
	if n := inputInt(op, "artifact_min_cast_events"); n > 0 {
		if err := assertArtifactMinCastEvents(artifact, n); err != nil {
			return err
		}
	}
	if want := inputString(op, "artifact_contains_text"); want != "" {
		if err := assertArtifactContainsText(artifact, want); err != nil {
			return err
		}
	}
	return nil
}

// inputString / inputInt / inputBool read typed values from the desugared
// plugin input map. Numbers tolerate the int/float64 split a JSON round-trip
// introduces (the Op crosses the plugin boundary as JSON).
func inputString(op *spec.Op, key string) string {
	if op.PluginInput == nil {
		return ""
	}
	s, _ := op.PluginInput[key].(string)
	return s
}

func inputInt(op *spec.Op, key string) int {
	if op.PluginInput == nil {
		return 0
	}
	switch v := op.PluginInput[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func inputBool(op *spec.Op, key string) bool {
	if op.PluginInput == nil {
		return false
	}
	b, _ := op.PluginInput[key].(bool)
	return b
}

// assertArtifactMinDimensions decodes the artifact's image header (PNG/JPEG) and
// fails if width or height is below the "WxH" requirement. Cheap — reads only
// the header via image.DecodeConfig, not the full pixel data.
func assertArtifactMinDimensions(path, wxh string) error {
	parts := strings.SplitN(wxh, "x", 2)
	if len(parts) != 2 {
		return fmt.Errorf("artifact_min_dimensions: bad format %q (want WxH)", wxh)
	}
	wantW, err := strconv.Atoi(parts[0])
	if err != nil || wantW <= 0 {
		return fmt.Errorf("artifact_min_dimensions: bad width %q", parts[0])
	}
	wantH, err := strconv.Atoi(parts[1])
	if err != nil || wantH <= 0 {
		return fmt.Errorf("artifact_min_dimensions: bad height %q", parts[1])
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("artifact %q open: %w", path, err)
	}
	defer f.Close() //nolint:errcheck
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return fmt.Errorf("artifact %q decode-config: %w", path, err)
	}
	if cfg.Width < wantW || cfg.Height < wantH {
		return fmt.Errorf("artifact %q dimensions %dx%d < required min %dx%d", path, cfg.Width, cfg.Height, wantW, wantH)
	}
	return nil
}

// assertArtifactNotUniform decodes the full image and samples pixels at 100
// deterministic positions; fails if every sampled pixel shares the same RGBA.
// Catches all-black / all-white / blank-canvas captures that a byte-size check
// alone would pass.
func assertArtifactNotUniform(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("artifact %q open: %w", path, err)
	}
	defer f.Close() //nolint:errcheck
	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("artifact %q decode: %w", path, err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("artifact %q has zero-size bounds %dx%d", path, w, h)
	}
	stepX := max(w/10, 1)
	stepY := max(h/10, 1)
	var firstR, firstG, firstB, firstA uint32
	first := true
	for py := bounds.Min.Y; py < bounds.Max.Y; py += stepY {
		for px := bounds.Min.X; px < bounds.Max.X; px += stepX {
			r, g, b, a := img.At(px, py).RGBA()
			if first {
				firstR, firstG, firstB, firstA = r, g, b, a
				first = false
				continue
			}
			if r != firstR || g != firstG || b != firstB || a != firstA {
				return nil // found a varying pixel — not uniform
			}
		}
	}
	return fmt.Errorf("artifact %q is uniformly one color (RGBA=%d,%d,%d,%d) — likely a blank/black/white capture",
		path, firstR>>8, firstG>>8, firstB>>8, firstA>>8)
}

// assertArtifactMinCastEvents validates an asciinema .cast file has at least
// minEvents event lines after a valid v2 header line.
func assertArtifactMinCastEvents(path string, minEvents int) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("artifact %q open: %w", path, err)
	}
	defer f.Close() //nolint:errcheck
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !scan.Scan() {
		return fmt.Errorf("artifact %q is empty (expected asciinema cast header on line 1)", path)
	}
	var header map[string]any
	if err := json.Unmarshal(scan.Bytes(), &header); err != nil {
		return fmt.Errorf("artifact %q line 1: not a JSON object (asciinema header expected): %w", path, err)
	}
	if _, ok := header["version"]; !ok {
		return fmt.Errorf("artifact %q line 1: JSON object missing %q field (not an asciinema cast header)", path, "version")
	}
	events := 0
	for scan.Scan() {
		if len(strings.TrimSpace(scan.Text())) == 0 {
			continue
		}
		events++
		if events >= minEvents {
			return nil
		}
	}
	if err := scan.Err(); err != nil {
		return fmt.Errorf("artifact %q scan: %w", path, err)
	}
	return fmt.Errorf("artifact %q has %d events, want >= %d", path, events, minEvents)
}

// ocrUpscale is the factor the artifact is enlarged by before OCR. Tesseract
// drops small UI text at native resolution: measured on a 1280x800 desktop
// capture whose bar reads "Monday 10:52" plainly to a human, tesseract returned
// ZERO words at 1x, 2x and 3x, and read the clock correctly at 8x. Desktop
// captures are screen-resolution, not document-resolution, and tesseract wants
// roughly the latter.
const ocrUpscale = 4

// assertArtifactContainsText runs OCR over the artifact and asserts the wanted
// text appears in it, case-insensitively.
//
// It is the assertion that separates "a surface mapped" from "a surface
// RENDERED": a layer can be present, sized and on-screen while drawing nothing,
// and every other artifact validator here would pass it. min_bytes passes on a
// blank PNG, not_uniform passes on a wallpaper with no text at all.
//
// FAILING TO RUN IS NOT FAILING TO MATCH. If tesseract is absent, or its
// language data is missing, this returns an error that says so instead of
// reporting the text as not found — the distinction cost real debugging time to
// learn: with eng.traineddata absent, tesseract writes its complaint to STDERR
// and exits with EMPTY stdout, so a caller that discards stderr sees exactly
// what a genuine no-match looks like. A check verb that cannot tell "I looked
// and it wasn't there" from "I could not look" is the failure-open shape this
// SDK has been bitten by before.
func assertArtifactContainsText(path, want string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("artifact %q not found: %w", path, err)
	}
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		return fmt.Errorf("artifact_contains_text needs the `tesseract` OCR engine on the HOST "+
			"(this validator runs host-side, on the pulled artifact) and it is not on PATH: %w", err)
	}

	scaled, cleanup, err := upscaleForOCR(path)
	if err != nil {
		return err
	}
	defer cleanup()

	// --psm 11 is sparse-text mode: a desktop capture is scattered labels, not a
	// page of prose, and the default page-segmentation model finds little in it.
	cmd := exec.Command(bin, scaled, "stdout", "--psm", "11")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	// Tesseract reports a missing language pack on stderr and still exits 0 in
	// some builds, so the stderr check comes FIRST and is not conditional on the
	// exit status.
	if msg := stderr.String(); strings.Contains(msg, "Error opening data file") ||
		strings.Contains(msg, "Failed loading language") {
		return fmt.Errorf("artifact_contains_text: tesseract has no usable language data, so OCR "+
			"never ran (install the eng data pack, e.g. tesseract-data-eng): %s", strings.TrimSpace(msg))
	}
	if runErr != nil {
		return fmt.Errorf("artifact_contains_text: tesseract failed on %q: %w (stderr: %s)",
			path, runErr, strings.TrimSpace(stderr.String()))
	}

	got := stdout.String()
	if strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
		return nil
	}
	return fmt.Errorf("artifact_contains_text: %q not found in the text OCR read from %q "+
		"(read %d characters; first 200: %q)", want, path, len(got), truncateForError(got, 200))
}

// upscaleForOCR writes an enlarged copy of the artifact and returns its path
// plus a cleanup. Nearest-neighbour keeps glyph edges hard, which is what
// tesseract thresholds against; a smoothing filter blurs thin UI type.
func upscaleForOCR(path string) (string, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return "", func() {}, fmt.Errorf("artifact_contains_text: open %q: %w", path, err)
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return "", func() {}, fmt.Errorf("artifact_contains_text: decode %q: %w", path, err)
	}

	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*ocrUpscale, b.Dy()*ocrUpscale))
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			dst.Set(x, y, src.At(b.Min.X+x/ocrUpscale, b.Min.Y+y/ocrUpscale))
		}
	}

	out, err := os.CreateTemp("", "charly-ocr-*.png")
	if err != nil {
		return "", func() {}, fmt.Errorf("artifact_contains_text: temp file: %w", err)
	}
	name := out.Name()
	cleanup := func() { os.Remove(name) }
	if err := png.Encode(out, dst); err != nil {
		out.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("artifact_contains_text: encode upscaled copy: %w", err)
	}
	out.Close()
	return name, cleanup, nil
}

func truncateForError(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
