// screen.go is the TRANSPORT-AGNOSTIC screen-fingerprint primitive: a perceptual
// hash of a captured frame, so a console action can trigger when the CURRENT
// screen MATCHES A REFERENCE screenshot — without OCR and without a text anchor.
//
// WHY: some screens are far easier to recognise by their PIXELS than by OCR. A
// firmware menu, a graphical lock screen, a splash — these OCR badly or not at
// all, but they are visually stable. A difference hash (dHash) reduces a frame to
// 64 bits that survive small rendering differences (a clock tick, a cursor) while
// distinguishing different screens. Two frames of the same screen are a few bits
// apart; two different screens are far apart. So "trigger when the screen equals
// the reference" becomes a Hamming-distance test with a threshold.
//
// It uses ONLY the standard library (image + image/png) — no new dependency, and
// no network. The hash is deterministic, so a reference captured once is portable
// across runs.

package kit

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register the JPEG decoder so a captured JPEG frame can be hashed
	_ "image/png"
	"os"
)

// ScreenSignature is a perceptual fingerprint of one frame: a 64-bit difference
// hash plus the source geometry. Two frames of the SAME screen have a small
// Hamming distance; different screens have a large one.
type ScreenSignature struct {
	// Hash is the 64-bit dHash (row-major adjacent-pixel comparisons).
	Hash uint64
	// Width / Height are the source frame dimensions, carried for evidence and
	// for a sanity check (a wildly different geometry is reported).
	Width, Height int
}

// screenHashWidth / screenHashHeight are the fixed grid the frame is reduced to
// before hashing: 9x8 pixels yields 8x8 = 64 adjacent-pixel comparisons.
const (
	screenHashWidth  = 9
	screenHashHeight = 8
)

// ScreenHash computes a frame's perceptual signature. It decodes the PNG/JPEG,
// reduces it to a 9x8 grayscale grid (nearest-neighbour, so glyph/thin-line edges
// stay hard), and sets one bit per horizontal neighbour comparison (left<right).
// PURE and deterministic over the input bytes.
func ScreenHash(frame []byte) (ScreenSignature, error) {
	src, _, err := image.Decode(bytes.NewReader(frame))
	if err != nil {
		return ScreenSignature{}, fmt.Errorf("screen hash: decode frame: %w", err)
	}
	b := src.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return ScreenSignature{}, fmt.Errorf("screen hash: decoded frame has zero size")
	}
	// Reduce to the grid by nearest-neighbour sampling.
	var gray [screenHashHeight][screenHashWidth]uint8
	for gy := 0; gy < screenHashHeight; gy++ {
		for gx := 0; gx < screenHashWidth; gx++ {
			px := b.Min.X + gx*b.Dx()/screenHashWidth
			py := b.Min.Y + gy*b.Dy()/screenHashHeight
			r, g, bl, _ := src.At(px, py).RGBA()
			// Rec. 601 luma, on the 0..255 scale.
			lum := (299*r + 587*g + 114*bl) / 1000
			gray[gy][gx] = uint8(lum >> 8)
		}
	}
	var hash uint64
	bit := 0
	for gy := 0; gy < screenHashHeight; gy++ {
		for gx := 0; gx < screenHashWidth-1; gx++ {
			if gray[gy][gx] < gray[gy][gx+1] {
				hash |= 1 << uint(bit)
			}
			bit++
		}
	}
	return ScreenSignature{Hash: hash, Width: b.Dx(), Height: b.Dy()}, nil
}

// LoadScreenSignature reads a reference PNG/JPEG from the host path and hashes it.
// The reference is a "previously made screenshot" captured on the same machine.
func LoadScreenSignature(path string) (ScreenSignature, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ScreenSignature{}, fmt.Errorf("screen reference: reading %s: %w", path, err)
	}
	return ScreenHash(b)
}

// HammingDistance returns the number of differing bits between two signatures
// (0 = identical fingerprint, 64 = opposite).
func HammingDistance(a, b ScreenSignature) int {
	x := a.Hash ^ b.Hash
	n := 0
	for x != 0 {
		n += int(x & 1)
		x >>= 1
	}
	return n
}

// ScreenMatches reports whether the current frame's signature is within maxDistance
// of the reference (0..64). A maxDistance of 0 means identical fingerprints; a
// small value (e.g. 5) tolerates a clock tick, a cursor blink, or minor rendering
// noise. A negative maxDistance uses ScreenDefaultMaxDistance.
//
// GEOMETRY GUARD: when both signatures carry dimensions and they differ by more
// than a factor of 2, the frames are almost certainly different screens (a
// resolution change / a different display) — reported as no-match regardless of
// the hash, rather than a coincidental near-hash.
func ScreenMatches(current, reference ScreenSignature, maxDistance int) bool {
	if maxDistance < 0 {
		maxDistance = ScreenDefaultMaxDistance
	}
	if current.Width > 0 && reference.Width > 0 {
		if !withinFactor2(current.Width, reference.Width) || !withinFactor2(current.Height, reference.Height) {
			return false
		}
	}
	return HammingDistance(current, reference) <= maxDistance
}

func withinFactor2(a, b int) bool {
	if a < b {
		a, b = b, a
	}
	return b > 0 && a <= 2*b
}

// ScreenDefaultMaxDistance is the default Hamming threshold for a reference match
// — tolerant of small rendering differences without matching a different screen.
const ScreenDefaultMaxDistance = 5

// ScreenPreview renders a signature for an evidence line.
func ScreenPreview(s ScreenSignature) string {
	return fmt.Sprintf("%dx%d hash=%016x", s.Width, s.Height, s.Hash)
}
