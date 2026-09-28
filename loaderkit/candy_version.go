package loaderkit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// candy_version.go — the per-entity candy-version ARBITER (K1-proper sub-phase A,
// relocated verbatim from charly/layers.go). This is a genuinely kind-blind
// MECHANISM (boundary-law clause M): given the candidate materializations of one
// bare candy ref, it picks the winner by the candy's own per-entity `version:`
// field with git-tag freshness as the tiebreak. Its only dependencies are
// kit.CompareCalVer / kit.CompareSemver (semver/CalVer comparison) and
// spec.ScannedCandy — zero charly-core coupling — so it lives in loaderkit
// beside the candy-scan machinery it serves (ScanRemoteCandy), not in the host.
// The host's charly/layers.go scanCandyFromLocal remote-fetch orchestration
// (loader+refs front-end, core-private) calls loaderkit.PickCandyVersion.

// CandyCandidate (the fetched-materialization DATA) moved to spec (loadmodel.go) with the
// loader-result family; PickCandyVersion — the ARBITER — stays here because it needs kit's
// semver/CalVer comparison (a mechanism).

// PickCandyVersion arbitrates the candidates of ONE bare ref by their source git tag
// (the ONLY version — the authored per-entity `version:` is GONE with the
// schema-versioning removal cutover). The newest CalVer git tag wins; a candidate whose
// tag does not parse as CalVer sorts lowest (CompareCalVer falls back lexically). This is
// the sole candy-version arbiter — direct and transitive refs both flow through it.
// cands is non-empty.
//
// warn receives the skew advisory (when the candidates resolve to DIFFERENT git tags
// carrying DIFFERENT candy content). It is a REQUIRED parameter, not an optional shim.
// Every caller states where its advisories go; passing nil selects stderr explicitly
// rather than by omission.
func PickCandyVersion(bareRef string, cands []spec.CandyCandidate, warn func(string, ...any)) spec.CandyCandidate {
	best := cands[0]
	for _, c := range cands[1:] {
		if kit.CompareCalVer(c.GitTag, best.GitTag) > 0 {
			best = c // newer source git tag
		}
	}
	// NO ADVISORY FOR A RE-TAG OF IDENTICAL CONTENT. A hub repo re-tags an UNCHANGED
	// candy far more often than it changes THIS candy: a sibling candy landing in the
	// same repo mints a new tag, and tag-on-merge re-mints the tag at every merge.
	// Because the candidate materializations are keyed by (repo, tag) and the whole
	// closure is re-fetched, one skewing hub turns into hundreds of "using newest X,
	// ignoring Y" lines that name a difference which does not exist in the bytes. The
	// winner is still the newest tag — for byte-identical content the choice is moot —
	// so the line is pure noise and is now emitted ONLY when the candidates' content
	// genuinely DIFFERS (or cannot be determined). THAT is the case a reader can act
	// on: a real cross-tag content divergence.
	if !candyContentsIdentical(cands) {
		for _, c := range cands {
			if c.GitTag != best.GitTag {
				if warn == nil {
					warn = func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
				}
				warn("Warning: candy %s resolved to multiple git tags with differing content; using newest %s (from %s), ignoring %s (from %s)",
					bareRef, best.GitTag, best.Source, c.GitTag, c.Source)
				break
			}
		}
	}
	return best
}

// candyContentsIdentical reports whether every candidate materialization carries the
// SAME candy content. Identity is, in order of preference:
//
//  1. the sha256 of the candy's own charly.yml at its fetched materialization
//     directory (the authored source — the ground truth for "is this the same candy?"), or
//  2. when that file is not readable, a canonical JSON digest of the resolved scan with
//     Model.SourceDir excluded (the cache path embeds the git tag, so it is NOT content).
//
// A candidate whose content cannot be determined (no manifest AND a zero scan) makes the
// whole set "not identical", so an unprovable pair still warns — never a silent
// suppression. That is the conservative direction: the absence of proof is not proof of
// sameness.
func candyContentsIdentical(cands []spec.CandyCandidate) bool {
	if len(cands) < 2 {
		return true
	}
	first, ok := candyContentIdentity(cands[0])
	if !ok {
		return false
	}
	for _, c := range cands[1:] {
		id, ok := candyContentIdentity(c)
		if !ok || id != first {
			return false
		}
	}
	return true
}

// candyContentIdentity returns the content digest of one candidate, or ok=false when no
// content signal exists at all.
func candyContentIdentity(c spec.CandyCandidate) (string, bool) {
	if dir := c.Scanned.Model.SourceDir; dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, spec.UnifiedFileName)); err == nil {
			return digestBytes(b), true
		}
	}
	sc := c.Scanned
	sc.Model.SourceDir = "" // the cache path embeds the tag; strip it from the content digest.
	b, err := json.Marshal(sc)
	if err != nil {
		return "", false
	}
	if string(b) == string(zeroScannedJSON) {
		return "", false // no content signal — cannot prove identity, so warn.
	}
	return digestBytes(b), true
}

// zeroScannedJSON is the canonical JSON of a zero-value ScannedCandy — the sentinel that
// distinguishes "a real scan whose content happens to be empty" from "no scan at all".
var zeroScannedJSON = func() []byte {
	b, _ := json.Marshal(spec.ScannedCandy{})
	return b
}()

func digestBytes(b []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
