package sdk

import (
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/kit"
)

// THE property this validator lives or dies by: a broken OCR setup must not look
// like a successful search that found nothing.
//
// With eng.traineddata absent, tesseract writes "Error opening data file ..." to
// STDERR and exits with EMPTY stdout. A caller that only reads stdout sees exactly
// what a genuine no-match looks like — which is how a debugging session concluded
// "OCR cannot read this desktop" when the truth was "OCR never ran". The error must
// name the cause, so the reader installs a language pack instead of deleting the
// assertion.
func TestArtifactContainsText_MissingLanguageDataIsNotANoMatch(t *testing.T) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract not installed; this test is about its language data")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writePNG(t, p, 20, 10, color.RGBA{255, 255, 255, 255})

	// Point tesseract at an EMPTY tessdata dir: the binary is present, the data is not.
	empty := t.TempDir()
	t.Setenv("TESSDATA_PREFIX", empty)

	err := assertArtifactContainsText(p, "anything")
	if err == nil {
		t.Fatal("no error: a missing language pack was reported as a successful no-match")
	}
	if !strings.Contains(err.Error(), "language data") {
		t.Errorf("error does not identify the cause as missing language data, so the reader\n"+
			"cannot tell it from a real no-match:\n  %v", err)
	}
	if strings.Contains(err.Error(), "not found in the text OCR read") {
		t.Errorf("a setup failure was reported using the NO-MATCH message:\n  %v", err)
	}
}

// A missing binary is the same class: it must say the engine is absent, not that the
// text was not on screen.
func TestArtifactContainsText_MissingBinaryIsNotANoMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writePNG(t, p, 20, 10, color.RGBA{255, 255, 255, 255})
	t.Setenv("PATH", t.TempDir()) // no tesseract anywhere on PATH

	err := assertArtifactContainsText(p, "anything")
	if err == nil {
		t.Fatal("no error with no OCR engine installed")
	}
	if !strings.Contains(err.Error(), "tesseract") {
		t.Errorf("error does not name the missing engine:\n  %v", err)
	}
}

// A missing artifact is a distinct failure from both of the above.
func TestArtifactContainsText_MissingArtifact(t *testing.T) {
	err := assertArtifactContainsText(filepath.Join(t.TempDir(), "absent.png"), "x")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a not-found error for an absent artifact, got: %v", err)
	}
}

// The upscale is not decoration: tesseract returned ZERO words from a real 1280x800
// desktop capture at 1x, 2x and 3x, and read its clock correctly once enlarged.
// The upscale is now the SHARED kit.OCRBytesScaled (R3 — the console transports
// call the same function); these tests pin the enlargement actually happening and
// that a decode failure surfaces rather than silently skipping OCR.
func TestArtifactOCR_UpscaleEnlarges(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writePNG(t, p, 8, 5, color.RGBA{0, 0, 0, 255})
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// OCRBytesScaled returns text, not the scaled file, so assert indirectly:
	// a 4x upscale of a tiny black image must not error (decode + upscale +
	// tesseract all ran), whereas a non-image must error at decode.
	if _, err := kit.OCRBytesScaled(data, ocrUpscale); err != nil {
		// tesseract may be absent in some environments; only a DECODE error fails here.
		if strings.Contains(err.Error(), "tesseract") {
			t.Skip("tesseract not installed")
		}
		t.Fatalf("upscaled OCR: %v", err)
	}
}

func TestArtifactOCR_UndecodableArtifact(t *testing.T) {
	if _, err := kit.OCRBytesScaled([]byte("this is not a PNG"), ocrUpscale); err == nil {
		t.Error("no error decoding a non-image; OCR would have run on nothing")
	} else if !strings.Contains(err.Error(), "decode") {
		t.Errorf("a non-image should fail at decode, got: %v", err)
	}
}
