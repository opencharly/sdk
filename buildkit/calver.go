package buildkit

import "github.com/opencharly/spec/spec"

// calver.go — the canonical CalVer build-tag computation. #55 import-purity: the BODY relocated DOWN
// to spec (spec/calver.go) so charly core reaches it over its spec+proto-only import surface. These
// thin var-forwarders keep buildkit's plugin callers (candy/plugin-build etc., which stamp the tag
// when the host leaves it empty) reading the ONE source unchanged (R3).

// ComputeCalVer returns the canonical build tag for the current UTC instant.
var ComputeCalVer = spec.ComputeCalVer

// ComputeCalVerAt formats t as the canonical CalVer (see spec.ComputeCalVerAt).
var ComputeCalVerAt = spec.ComputeCalVerAt
