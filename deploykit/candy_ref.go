package deploykit

import "strings"

// candy_ref.go — the runtime candy-reference MODEL type + its pure ref-string
// parsers. Moved to sdk/deploykit in P4 (with the runtime Candy graph it lives on);
// the clone/fetch LOGIC (EnsureRepoDownloaded, git ops) stays in charly/refs.go (P7).
// spec.CandyRef (= string) is the distinct CUE-AUTHORED ref form.

// CandyRef is a single candy reference as authored in the candy manifest `require:` /
// `candy:` (or charly.yml `candy:`). It carries the ORIGINAL ref string — with
// any `@repo` prefix and `:version` suffix — as the single source of truth; the
// bare map-key form (.Bare()) and the pinned version (.Version()) are DERIVED on
// demand, so a ref's identity and its version can never drift apart.
type CandyRef struct {
	Raw string // original ref, e.g. "@github.com/org/repo/candy/x:v1" or bare "x"
	// Resolved is the qualified candy-map key assigned when a freshly-fetched
	// remote candy's plain-name sibling deps are qualified to
	// "<repo>/<subpathprefix><name>" (qualifyRemoteSiblingDeps). Empty for every
	// other ref, where the map key derives from Raw.
	Resolved string
}

// Bare returns the candy-map key (no @ prefix, no :version) — the form used for
// dependency resolution and graph keying.
func (r CandyRef) Bare() string {
	if r.Resolved != "" {
		return r.Resolved
	}
	return BareRef(r.Raw)
}

// Version returns the pinned version (the ":vX" suffix), or "" for an unpinned
// remote ref or a local (bare-name) ref.
func (r CandyRef) Version() string { _, v := StripVersion(r.Raw); return v }

// IsRemote reports whether this is an @-prefixed remote ref.
func (r CandyRef) IsRemote() bool { return IsRemoteCandyRef(r.Raw) }

// ToCandyRefs wraps raw ref strings into CandyRef values. Returns nil for a
// nil/empty input so an absent list stays absent.
func ToCandyRefs(raw []string) []CandyRef {
	if len(raw) == 0 {
		return nil
	}
	out := make([]CandyRef, len(raw))
	for i, s := range raw {
		out[i] = CandyRef{Raw: s}
	}
	return out
}

// StripVersion splits an @-prefixed ref into its bare form and pinned version.
// A non-@ ref returns (ref, "").
func StripVersion(ref string) (string, string) {
	if !strings.HasPrefix(ref, "@") {
		return ref, ""
	}
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		return ref[:idx], ref[idx+1:]
	}
	return ref, ""
}

// IsRemoteCandyRef reports whether a ref is an @-prefixed remote ref.
func IsRemoteCandyRef(ref string) bool {
	return strings.HasPrefix(ref, "@")
}

// BareRef returns the map-key form of a ref (no @ prefix, no :version).
func BareRef(ref string) string {
	bare, _ := StripVersion(ref)
	return strings.TrimPrefix(bare, "@")
}
