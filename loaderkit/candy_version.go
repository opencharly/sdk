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

// PickCandyVersion arbitrates the candidates of ONE bare ref by per-entity
// version. Same per-entity version across different git tags => NO warning, the
// newest git tag wins (freshness). Different per-entity versions => warn once
// (naming the winner + a loser) and the newest per-entity version wins. This is
// the sole candy-version arbiter — direct and transitive refs both flow through
// it. cands is non-empty.
// PickCandyVersion keeps the original two-argument shape so existing callers and their tests
// compile unchanged; the advisory goes to stderr exactly as before.
func PickCandyVersion(bareRef string, cands []spec.CandyCandidate) spec.CandyCandidate {
	return PickCandyVersionWith(bareRef, cands, nil)
}

// PickCandyVersionWith is the same arbiter with an injectable advisory sink.
//
// The skew advisory used to be a bare `fmt.Fprintf(os.Stderr, ...)`, which made it
// unstructured and therefore UNCOUNTABLE: `charly box validate` could not report how many
// warnings a run produced because they never reached its diagnostics, so a summary line could
// only omit the number or state a false one. A caller that wants the advisory as DATA passes a
// collector here; nil falls back to stderr, so behaviour is unchanged for everyone else.
func PickCandyVersionWith(bareRef string, cands []spec.CandyCandidate, warn func(string, ...any)) spec.CandyCandidate {
	best := cands[0]
	for _, c := range cands[1:] {
		if kit.CompareCalVer(c.Version, best.Version) > 0 {
			best = c // newer per-entity version
		} else if c.Version == best.Version && kit.CompareSemver(c.GitTag, best.GitTag) > 0 {
			best = c // same per-entity version: prefer the newest git tag
		}
	}
	for _, c := range cands {
		if c.Version != best.Version {
			if warn == nil {
				warn = func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
			}
			warn("Warning: candy %s resolved to multiple versions; using newest %s (from %s), ignoring %s (from %s)",
				bareRef, best.Version, best.Source, c.Version, c.Source)
			break
		}
	}
	return best
}
