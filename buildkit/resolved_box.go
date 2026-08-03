package buildkit

import (
	"github.com/opencharly/spec/spec"
)

// Aliases onto the CUE-sourced spec types ResolvedBox references (DistroDef /
// DistroConfig / BuilderConfig are already bound in format_config.go, same
// package). These read unchanged in the moved code.
type (
	MergeConfig         = spec.MergeConfig
	ResolvedInit        = spec.ResolvedInit
	BuilderMap          = spec.BuilderMap
	AggregatedCandyCaps = spec.AggregatedCandyCaps
)

// ResolvedBox represents a fully resolved box configuration. #55 step3 unit 3a: the
// wire-clean value fields (Name/Version/Distro/Tags/User/…) moved to spec.ResolvedBox
// (spec/spec/resolved_box.go) — the shared contract type charly core's render-seam floor
// + the "resolved-project" envelope projector + candy/plugin-build all read/construct.
// SPIKE (value-type relocation, #55 keystone): the embedding wrapper itself moved to
// spec.BuildResolvedBox (spec/spec/build_resolved_box.go) — every one of its fields
// already resolved to a spec.* type (DistroConfig/BuilderConfig/ResolvedDistro/
// ResolvedInit/AggregatedCandyCaps/BakedLabelSet are all CUE-generated), so the wrapper
// carried zero buildkit-only content. This is now a zero-churn alias — every existing
// buildkit/deploykit/candy call site (composite literals, field selectors) compiles
// unchanged.
type ResolvedBox = spec.BuildResolvedBox
