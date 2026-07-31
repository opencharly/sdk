package kit

import "github.com/opencharly/spec/checkhost"

// check_endpoint.go — EndpointForVenue + CheckEndpoint RELOCATED to the spec fabric slice
// github.com/opencharly/spec/checkhost (#55 CHECK-ENGINE cone Option A — the check-verb
// host-vantage resolution family: net/ssh host primitives belong in a spec fabric slice, so charly
// core's check dispatch reaches them importing zero kit). The kind-blind resolver + its
// containerPublishedAddr / sshForwardEndpoint bodies live there; kit re-exports the exported surface
// so every existing kit.EndpointForVenue / kit.CheckEndpoint call site (the candies + sdk) is
// untouched. New consumers reference spec/checkhost directly.
type CheckEndpoint = checkhost.CheckEndpoint

var EndpointForVenue = checkhost.EndpointForVenue
