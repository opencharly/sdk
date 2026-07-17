package deploykit

import "github.com/opencharly/sdk/spec"

// candy_ref.go — CandyRef is now promoted to spec.CandyRefEntry (W9: the mass-edit interface
// relocation, alongside CandyModel → spec.CandyReader). spec.CandyRef already names the DISTINCT
// CUE-authored wire form (a bare string); CandyRefEntry is the runtime model with derived
// .Bare()/.Version()/.IsRemote() methods — still reachable through this alias unchanged. The pure
// ref-string helpers below (BareRef/StripVersion/IsRemoteCandyRef/ToCandyRefs) keep their
// established names and signatures (candy/plugin-box + several charly files call them by these
// names) — forwarding to the promoted spec functions so the logic has exactly one home.
type CandyRef = spec.CandyRefEntry

// ToCandyRefs wraps raw ref strings into CandyRef values. Returns nil for a
// nil/empty input so an absent list stays absent.
func ToCandyRefs(raw []string) []CandyRef { return spec.ToCandyRefEntries(raw) }

// StripVersion splits an @-prefixed ref into its bare form and pinned version.
// A non-@ ref returns (ref, "").
func StripVersion(ref string) (string, string) { return spec.StripCandyRefVersion(ref) }

// IsRemoteCandyRef reports whether a ref is an @-prefixed remote ref.
func IsRemoteCandyRef(ref string) bool { return spec.IsRemoteCandyRefString(ref) }

// BareRef returns the map-key form of a ref (no @ prefix, no :version).
func BareRef(ref string) string { return spec.BareCandyRef(ref) }
