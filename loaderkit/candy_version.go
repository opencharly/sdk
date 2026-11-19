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
	"github.com/opencharly/spec/calver"
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
// cands may be EMPTY: an unresolvable ref (no candidate materialization at all) emits
// DiagWarning and returns the zero candidate; a resolvable set emits at most a DiagInfo
// scope-conflict notice.
//
// SCOPE — the arbitration is a within-one-box concern, NOT a global one. A bare ref is
// composed by MANY independent, immutable boxes across the assembled closure (charly's
// boxes + every imported `distro-*` namespace), and two such boxes legitimately pin the
// same candy at different tags. That is NOT a conflict: each box resolves its own
// composition, no single box ever saw both versions, and the closure is still
// deterministic. Only when ≥2 referrers INSIDE THE SAME BOX name different tags is there a
// genuine conflict worth reporting (spec.ScopeIsBox; a shared layer or kind:local template is
// NOT a box, so its own difference is informational). Scope is
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
		if newerCandidate(c, best) {
			best = c // newer source git tag
		}
	}
	// NO ADVISORY unless SOME SCOPE's own references disagree AND their content genuinely
	// DIFFERS. Two independent filters, both required:
	//
	//  1. SCOPE (scopeConflicts): a version difference between references that share no BOX
	//     scope is the normal shape of a multi-box closure and is silent by construction. Only a
	//     box scope whose OWN references name >=2 distinct tags is a genuine conflict — and it is
	//     reported independently of which scope supplied the global winner. A non-box scope (a
	//     layer's own require: list, a kind:local template) is an independent composition, not a
	//     conflict unit.
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
			"candy %s resolved to multiple git tags with differing content within one scope (%s); using newest referenced %s (from %s)%s%s",
			bareRef, strings.Join(conflicts, ", "), best.GitTag, best.Source, describeSkew(cands, best),
			nonCalverRemedy(cands, best, conflicts))
	}
	return best
}

// nonCalverRemedy renders the ACTION a reader must take when a ref that is NOT a CalVer tag -- a
// commit SHA, a branch -- lost the arbitration to a CalVer tag inside the SAME box scope. It is
// APPENDED to the one scope-conflict message rather than emitted as a second diagnostic, so the
// skew keeps exactly one line and every existing reader of that line still recognizes it.
//
// WHY THE MESSAGE AND NOT A DIFFERENT WINNER. newerCandidate's CalVer-over-non-CalVer preference is
// deliberate and must stay: it is the fix for opencharly/charly#1179, where a STALE pinned commit
// two hops away beat the release that carried the actual fix. Letting a SHA win again would reopen
// that regression, and would let a shared scope compose an arbitrary revision -- strictly worse
// than refusing. So the SHA loses, and the ONE thing missing is the sentence that says so: a
// reader who pinned a SHA saw "resolved to multiple git TAGS ... using newest referenced
// v2026.323.0252", which names neither the SHA's fate nor a way forward (opencharly/charly#1215,
// measured while trying to bed-prove opencharly/layer-cuda#28).
//
// The remedy is the producer-first order, because it is the only correct way to adopt an unmerged
// layer: the producer lands, merges and is tagged, and THEN the `require:` that names the tag moves
// to it, so no scope ever names two tags at once.
func nonCalverRemedy(cands []spec.CandyCandidate, best spec.CandyCandidate, conflicts []string) string {
	// The explanation only exists for a TAG beating a NON-tag. When the winner is not a CalVer tag
	// either, the skew is between two refs of the same kind and the generic sentence is the whole
	// truth.
	if _, ok := calver.ParseCalVer(strings.TrimPrefix(best.GitTag, "v")); !ok {
		return ""
	}
	inConflict := make(map[string]bool, len(conflicts))
	for _, s := range conflicts {
		inConflict[s] = true
	}
	type loser struct{ ref, scope, kind string }
	var losers []loser
	for i := range cands {
		c := cands[i]
		if c.GitTag == best.GitTag && c.Source == best.Source {
			continue
		}
		if _, ok := calver.ParseCalVer(strings.TrimPrefix(c.GitTag, "v")); ok {
			continue // another real tag: the generic sentence already covers it
		}
		kind := "a ref that is not a CalVer tag"
		if isCommitSHA(c.GitTag) {
			kind = "a revision with no CalVer to order it"
		}
		for _, s := range c.Referrers {
			if inConflict[s] {
				losers = append(losers, loser{ref: c.GitTag, scope: s, kind: kind})
			}
		}
	}
	if len(losers) == 0 {
		return ""
	}
	sort.Slice(losers, func(i, j int) bool {
		if losers[i].scope != losers[j].scope {
			return losers[i].scope < losers[j].scope
		}
		return losers[i].ref < losers[j].ref
	})
	var b strings.Builder
	for _, l := range losers {
		fmt.Fprintf(&b,
			"; %s pins %s, which is %s and cannot be arbitrated against a release tag: it carries no recency the CalVer order can read, so %s wins and %s is never composed. Move the `require:` that names %s to a release containing %s FIRST (producer-first: land it, merge it, tag it, then move the pin), or stop naming %s in %s",
			l.scope, l.ref, l.kind, best.GitTag, l.ref, best.GitTag, l.ref, best.GitTag, l.scope)
	}
	return b.String()
}

// isCommitSHA reports whether ref is a full 40-hex revision id -- the immutable pin the
// producer-first order cannot arbitrate, as distinct from a branch name (also non-CalVer, and
// named as such).
func isCommitSHA(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// newerCandidate reports whether a is a strictly newer candidate than b.
//
// A REAL CalVer TAG OUTRANKS A REF THAT IS NOT ONE. A bare commit SHA is a legitimate pin — the
// `distro-*` rule pins SHAs because those commits are untagged — but a SHA carries no recency,
// and the lenient comparator's lexical fallback puts "7766a9d06ec5" ABOVE "v2026.320.1938"
// ('7' > '2'), so a STALE SHA used to be chosen as "newest" and the old content won the
// arbitration with only an INFO line to say so. That is exactly the case where the loser is the
// fix: measured in opencharly/distro-cachyos, where the driver-shadowing fix at
// layer-nvidia v2026.320.1938 lost to the pre-fix commit pinned by a stale `require:` two hops
// away (opencharly/charly#1179).
//
// spec/calver.ParseCalVer already documents the intended semantics — "a false result is treated
// by the freshness comparison as older than every real CalVer" — so this restores it. Two refs
// that are BOTH non-CalVer keep the legacy lenient order, so a project that pins only SHAs is
// unaffected.
func newerCandidate(a, b spec.CandyCandidate) bool {
	// Git tags carry a leading "v"; the canonical CalVer form does not (the lenient comparator
	// trims it for the same reason). Without the trim EVERY tag parses as non-CalVer and this
	// preference would never apply.
	aCal, aOK := calver.ParseCalVer(strings.TrimPrefix(a.GitTag, "v"))
	bCal, bOK := calver.ParseCalVer(strings.TrimPrefix(b.GitTag, "v"))
	switch {
	case aOK && bOK:
		return bCal.Less(aCal)
	case aOK:
		return true
	case bOK:
		return false
	default:
		return kit.CompareCalVer(a.GitTag, b.GitTag) > 0
	}
}

// describeSkew renders EVERY candidate that lost the arbitration: its tag, the source ref it
// was materialized from, and the box scopes that named it. The diagnostic previously named only
// the WINNER, so a reader could not tell which refs disagreed or where the losing one came from —
// a comparison with one side hidden. That cost hours on a real pin advance
// (opencharly/charly#1179), where the loser turned out to be a stale `require:` two hops away.
// Appending the losers makes the message self-diagnosing without changing the arbitration.
func describeSkew(cands []spec.CandyCandidate, best spec.CandyCandidate) string {
	var b strings.Builder
	for _, c := range cands {
		if c.GitTag == best.GitTag && c.Source == best.Source {
			continue
		}
		scopes := append([]string(nil), c.Referrers...)
		sort.Strings(scopes)
		fmt.Fprintf(&b, "; also referenced %s (from %s", c.GitTag, c.Source)
		if len(scopes) > 0 {
			fmt.Fprintf(&b, "; scopes: %s", strings.Join(scopes, ", "))
		}
		b.WriteString(")")
	}
	return b.String()
}

// scopeConflicts returns the BOX scopes whose own references name >=2 DISTINCT git tags — the
// exact condition the version rule calls a conflict ("multiple layers inside the same box point
// to different versions"). A scope is conflict-ELIGIBLE only when it names a BOX
// (spec.ScopeIsBox): a box's candy closure ("box=<qualified-name>") is the ONE composition unit
// two candy references genuinely share and can therefore disagree within. Every other scope — an
// unattributable layer ("layer=<name>"), a kind:local template, a bare capability-connect scan
// (the empty scope) — is an INDEPENDENT composition with no box to conflict within, so it is
// SILENT by construction. Two references that share NO scope are likewise independent and never
// conflict. Result is sorted for a deterministic message.
func scopeConflicts(cands []spec.CandyCandidate) []string {
	byScope := map[string]map[string]bool{} // box scope label -> set of git tags referenced within it
	for i := range cands {
		for _, scope := range cands[i].Referrers {
			if !spec.ScopeIsBox(scope) {
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
