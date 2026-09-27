package loaderkit

import (
	"fmt"
	"os"

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
// warn receives the skew advisory (when the candidates resolve to DIFFERENT git tags).
// It is a REQUIRED parameter, not an optional shim. Every caller states where its
// advisories go; passing nil selects stderr explicitly rather than by omission.
func PickCandyVersion(bareRef string, cands []spec.CandyCandidate, warn func(string, ...any)) spec.CandyCandidate {
	best := cands[0]
	for _, c := range cands[1:] {
		if kit.CompareCalVer(c.GitTag, best.GitTag) > 0 {
			best = c // newer source git tag
		}
	}
	for _, c := range cands {
		if c.GitTag != best.GitTag {
			if warn == nil {
				warn = func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
			}
			warn("Warning: candy %s resolved to multiple git tags; using newest %s (from %s), ignoring %s (from %s)",
				bareRef, best.GitTag, best.Source, c.GitTag, c.Source)
			break
		}
	}
	return best
}
