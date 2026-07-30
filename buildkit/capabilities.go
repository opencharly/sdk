package buildkit

import "github.com/opencharly/spec/spec"

// capabilities.go — the candy `capabilities:` aggregation + required-capability check. #55
// import-purity: the BODIES relocated DOWN to spec (spec/candy_capabilities.go) so charly core reads
// them over its spec+proto-only import surface; these thin var-forwarders keep buildkit's build-resolve
// callers unchanged (R3).

// AggregateCandyCapabilities merges each candy's `capabilities:` contribution (see spec).
var AggregateCandyCapabilities = spec.AggregateCandyCapabilities

// CheckRequiredCapabilities returns the sorted missing `requires_capabilities:` names (see spec).
var CheckRequiredCapabilities = spec.CheckRequiredCapabilities

// CandyCapabilitiesError formats a missing-capabilities error (see spec).
var CandyCapabilitiesError = spec.CandyCapabilitiesError
