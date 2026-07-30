package buildkit

import "github.com/opencharly/spec/spec"

// externalized_builders.go — the D-FACT of which detection-builder words are served by an EXTERNAL
// out-of-process plugin. #55 import-purity: the VALUE relocated DOWN to spec
// (spec/externalized_builders.go) so charly core reads the ONE source over its spec+proto-only
// import surface; this thin var-forwarder keeps buildkit's build-resolve callers unchanged (R3).

// ExternalizedBuilders is THE single source of truth for which builder words are served by an EXTERNAL
// out-of-process plugin.
var ExternalizedBuilders = spec.ExternalizedBuilders
