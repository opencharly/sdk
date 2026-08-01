package buildkit

import "github.com/opencharly/spec/spec"

// phase_template.go — the (phase, venue) → template-string resolvers for the build-vocabulary
// Format / Builder. #55 import-purity: the BODIES relocated DOWN to spec (spec/phase_template.go) so
// charly core reads them over its spec+proto-only import surface; these thin var-forwarders keep
// buildkit's format/builder render callers (privileged_runner.go / system_packages.go) unchanged (R3).

// FormatPhaseTemplate looks up the (phase, venue) template string for a FormatDef (see
// spec.FormatPhaseTemplate). FormatDef = spec.Format, so the forwarded signature matches.
var FormatPhaseTemplate = spec.FormatPhaseTemplate

// BuilderPhaseTemplate is the BuilderDef analog of FormatPhaseTemplate (see spec.BuilderPhaseTemplate).
var BuilderPhaseTemplate = spec.BuilderPhaseTemplate
