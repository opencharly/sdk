package loaderkit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
// SCOPE — the arbitration is a within-one-box concern, NOT a global one. A bare ref is
// composed by MANY independent, immutable boxes across the assembled closure (charly's
// boxes + every imported `distro-*` namespace), and two such boxes legitimately pin the
// same candy at different tags. That is NOT a conflict: each box resolves its own
// composition, no single box ever saw both versions, and the closure is still
// deterministic. Only when ≥2 referrers INSIDE THE SAME SCOPE (the same box, or the same
// shared layer) name different tags is there a genuine conflict worth reporting. Scope is
// carried per-candidate (spec.CandyCandidate.Referrers, seeded from the reachability walk's
// RemoteDownload.RefReferrers); candidates whose referrers are unknown share no scope and
// are treated as independent.
//
// SEVERITY — a resolvable skew is INFO, never WARNING. The arbiter always has a
// deterministic winner (the newest referenced tag), so resolution SUCCEEDS and nothing is
// unresolved; a consumer's zero-warnings gate is for unresolved defects and must not fail
// on a successful arbitration. Only a genuinely unresolvable set (no candidate at all)
// emits DiagWarning. diag receives both the level and the message, so one sink — carrying
// the level as its first argument — covers every scan diagnostic with no second channel.
//
// diag is a REQUIRED parameter, not an optional shim. Every caller states where its
// diagnostics go; passing nil selects stderr explicitly rather than by omission.
func PickCandyVersion(bareRef string, cands []spec.CandyCandidate, diag func(spec.DiagLevel, string, ...any)) spec.CandyCandidate {
	if len(cands) == 0 {
		// Genuinely unresolvable: nothing to arbitrate. The ONLY warning-tier case —
		// there is no winner, so a consumer cannot proceed.
		emitDiag(diag, spec.DiagWarning, "candy %s has no candidate materialization to resolve", bareRef)
		return spec.CandyCandidate{}
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if kit.CompareCalVer(c.GitTag, best.GitTag) > 0 {
			best = c // newer source git tag
		}
	}
	// NO ADVISORY unless SOME SCOPE's own references disagree AND their content genuinely
	// DIFFERS. Two independent filters, both required:
	//
	//  1. SCOPE (scopeConflicts): a version difference between references that share no box/layer
	//     scope is the normal shape of a multi-box closure and is silent by construction. Only a
	//     scope whose OWN references name >=2 distinct tags is a genuine conflict — and it is
	//     reported independently of which scope supplied the global winner.
	//  2. CONTENT (candyContentsIdentical): a hub repo re-tags an UNCHANGED candy far more
	//     often than it changes THIS candy — a sibling landing in the same repo mints a new tag,
	//     and tag-on-merge re-mints the tag at every merge. The candidate materializations are
	//     keyed by (repo, tag) and the whole closure is re-fetched, so one skewing hub turns into
	//     hundreds of "using newest X, ignoring Y" lines that name a difference which does not
	//     exist in the bytes. The winner is still the newest tag — for byte-identical content the
	//     choice is moot — so the line is pure noise and is emitted ONLY when the candidates'
	//     content genuinely DIFFERS (or cannot be determined). THAT is the case a reader can act
	//     on: a real, same-scope, cross-tag content divergence.
	if conflicts := scopeConflicts(cands); len(conflicts) > 0 && !candyContentsIdentical(cands) {
		emitDiag(diag, spec.DiagInfo,
			"candy %s resolved to multiple git tags with differing content within one scope (%s); using newest referenced %s (from %s)",
			bareRef, strings.Join(conflicts, ", "), best.GitTag, best.Source)
	}
	return best
}

// scopeConflicts returns the SCOPE labels whose own references name >=2 DISTINCT git tags — the
// exact condition the version rule calls a conflict ("multiple layers inside the same box point to
// different versions"). A scope is a box's candy closure ("box=<qualified-name>"), a kind:local
// template ("kind:local=<tpl>"), an unattributable layer's own scope ("layer=<name>"), or the
// deploy overlay; two references that share NO scope are independent compositions and never
// conflict. Result is sorted for a deterministic message.
func scopeConflicts(cands []spec.CandyCandidate) []string {
	byScope := map[string]map[string]bool{} // scope label -> set of git tags referenced within it
	for i := range cands {
		for _, scope := range cands[i].Referrers {
			if scope == "" {
				continue
			}
			if byScope[scope] == nil {
				byScope[scope] = map[string]bool{}
			}
			byScope[scope][cands[i].GitTag] = true
		}
	}
	var out []string
	for scope, tags := range byScope {
		if len(tags) > 1 {
			out = append(out, scope)
		}
	}
	sort.Strings(out)
	return out
}

// emitDiag routes one diagnostic to the caller's sink at the given level. A nil sink selects
// stderr EXPLICITLY (the level picks the printed prefix: "Warning:" for a gate-failing warning,
// "Notice:" for a non-gating info), so the fallback preserves the pre-severity behaviour for
// every existing caller while the level travels with the message.
func emitDiag(diag func(spec.DiagLevel, string, ...any), level spec.DiagLevel, format string, args ...any) {
	if diag == nil {
		prefix := "Notice:"
		if level == spec.DiagWarning {
			prefix = "Warning:"
		}
		fmt.Fprintf(os.Stderr, prefix+" "+format+"\n", args...)
		return
	}
	diag(level, format, args...)
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
// whole set "not identical", so an unprovable pair still emits — never a silent
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
		return "", false // no content signal — cannot prove identity, so emit.
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
