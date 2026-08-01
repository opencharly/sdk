package kit

import "github.com/opencharly/spec/spec"

// descent.go — re-export of the descent stamper, RELOCATED to spec/spec/descent.go
// (#55 fabric-primitive/value extraction). StampDescent/DescentFromTraits are pure
// #Deploy-tree value transforms over the E-envelope, so they home in spec; kit re-exports
// them here so every existing kit.StampDescent / kit.DescentFromTraits call site (plugins +
// sdk) is untouched. New consumers should reference spec.StampDescent / spec.DescentFromTraits.
var (
	StampDescent      = spec.StampDescent
	DescentFromTraits = spec.DescentFromTraits
)
