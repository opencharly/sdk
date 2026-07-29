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

// CandyCandidate is one fetched materialization of a bare candy ref. The git tag
// is the fetch coordinate; Version is the candy's own per-entity `version:`.
type CandyCandidate struct {
	Scanned spec.ScannedCandy
	Version string // per-entity version (Scanned.Model.Version) — mandatory, never ""
	GitTag  string // fetch coordinate (the @github :vTAG)
	Source  string // "<repo>@<git-tag>" for warning attribution
}

// PickCandyVersion arbitrates the candidates of ONE bare ref by per-entity
// version. Same per-entity version across different git tags => NO warning, the
// newest git tag wins (freshness). Different per-entity versions => warn once
// (naming the winner + a loser) and the newest per-entity version wins. This is
// the sole candy-version arbiter — direct and transitive refs both flow through
// it. cands is non-empty.
func PickCandyVersion(bareRef string, cands []CandyCandidate) CandyCandidate {
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
			fmt.Fprintf(os.Stderr,
				"Warning: candy %s resolved to multiple versions; using newest %s (from %s), ignoring %s (from %s)\n",
				bareRef, best.Version, best.Source, c.Version, c.Source)
			break
		}
	}
	return best
}
