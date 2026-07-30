package kit

// loader_directives.go — the path-anchoring MECHANISM forwarder for discover scan specs. The
// kind-blind config-loader document DIRECTIVE TYPES (ImportEntry/ImportList, DiscoverConfig/ScanSpec)
// and the canonical manifest-filename constant moved to the types-only spec module
// (spec/load_directives.go, #55 Phase B) with the UnifiedFile they are fields of. The AnchorScanSpecs
// FUNCTION itself moved to that SAME spec module (#55 C3b) because its only non-loaderkit caller,
// MergeUnified, relocated into spec and spec cannot import sdk/kit; sdk/kit keeps this thin forwarder
// so its remaining in-kit caller (walk.go) reaches the ONE copy without a second definition (R3).

import "github.com/opencharly/spec/spec"

// AnchorScanSpecs forwards to spec.AnchorScanSpecs — the ONE path-anchoring
// mechanism, now resident in the spec module. Kept as a var alias so kit's own
// walk.go caller stays terse and there is exactly one implementation.
var AnchorScanSpecs = spec.AnchorScanSpecs
