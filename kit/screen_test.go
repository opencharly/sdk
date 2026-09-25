package kit

// screen_test.go pins the perceptual screen-fingerprint primitive: same screen →
// small distance, different screen → large, and the reference-match contract used
// to trigger an action from a previously-captured screenshot.

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// makePNG renders a WxH image with a deterministic pattern, so two calls with the
// same params are byte-identical and different params differ.
func makePNG(t *testing.T, w, h int, top, bottom color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		c := top
		if y >= h/2 {
			c = bottom
		}
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestScreenHash_SameIsZero proves an identical frame hashes identically.
func TestScreenHash_SameIsZero(t *testing.T) {
	f := makePNG(t, 100, 80, color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255})
	a, err := ScreenHash(f)
	if err != nil {
		t.Fatalf("ScreenHash: %v", err)
	}
	b, _ := ScreenHash(f)
	if HammingDistance(a, b) != 0 {
		t.Fatalf("identical frames must hash equal: %d", HammingDistance(a, b))
	}
	if a.Width != 100 || a.Height != 80 {
		t.Fatalf("geometry wrong: %+v", a)
	}
}

// TestScreenMatches_DifferentScreensDiffer proves two visually different screens
// are far apart and do NOT match at the default threshold. The two patterns are
// HORIZONTALLY structured (alternating columns) so their dHash — which compares
// horizontal neighbours — differs substantially.
func TestScreenMatches_DifferentScreensDiffer(t *testing.T) {
	left := makeStripesPNG(t, 100, 80, 10)  // vertical stripes, period 10
	right := makeStripesPNG(t, 100, 80, 25) // vertical stripes, period 25
	a, _ := ScreenHash(left)
	b, _ := ScreenHash(right)
	if ScreenMatches(a, b, -1) {
		t.Fatalf("different screens must not match at the default threshold (distance=%d)", HammingDistance(a, b))
	}
}

// makeStripesPNG renders vertical black/white stripes of the given period.
func makeStripesPNG(t *testing.T, w, h, period int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{0, 0, 0, 255}
			if (x/period)%2 == 0 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestScreenMatches_SameScreenWithinThreshold proves a near-identical frame (a
// small pixel change) matches the reference.
func TestScreenMatches_SameScreenWithinThreshold(t *testing.T) {
	ref := makePNG(t, 100, 80, color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255})
	// Same dominant pattern; the grid sampling is robust to a tiny corner change.
	cur := makePNG(t, 100, 80, color.RGBA{5, 5, 5, 255}, color.RGBA{250, 250, 250, 255})
	a, _ := ScreenHash(ref)
	b, _ := ScreenHash(cur)
	if !ScreenMatches(a, b, -1) {
		t.Fatalf("a near-identical frame must match (distance=%d)", HammingDistance(a, b))
	}
}

// TestScreenMatches_GeometryGuard proves a wild resolution difference is a
// no-match regardless of the hash.
func TestScreenMatches_GeometryGuard(t *testing.T) {
	a := ScreenSignature{Hash: 0, Width: 100, Height: 80}
	b := ScreenSignature{Hash: 0, Width: 1000, Height: 800} // >2x
	if ScreenMatches(a, b, 64) {
		t.Fatal("a >2x resolution difference must not match, even with an identical hash")
	}
}

// TestLoadScreenSignature proves a reference file loads and matches itself.
func TestLoadScreenSignature(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ref.png")
	if err := os.WriteFile(p, makePNG(t, 64, 48, color.RGBA{10, 20, 30, 255}, color.RGBA{200, 210, 220, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := LoadScreenSignature(p)
	if err != nil {
		t.Fatalf("LoadScreenSignature: %v", err)
	}
	cur, _ := ScreenHash(makePNG(t, 64, 48, color.RGBA{10, 20, 30, 255}, color.RGBA{200, 210, 220, 255}))
	if !ScreenMatches(cur, ref, -1) {
		t.Fatal("the frame must match its own reference file")
	}
	// A missing reference is a real error, never a non-match.
	if _, err := LoadScreenSignature(filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("a missing reference must be an error")
	}
}
