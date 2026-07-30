package loaderkit

import "github.com/opencharly/spec/spec"

// vocab.go — the SHARED build-vocabulary projections (ProjectDistroConfig / ProjectBuilderConfig /
// ProjectInitConfig) are DEFINED in the dedicated spec module (spec/spec/vocab_project.go, #55 2b
// Class A): they are pure over spec types + spec's own plugin-kind decoders, so spec is their single
// home. These forwarders keep loaderkit's callers — charly core (format_config.go) + candy/plugin-build
// (the build-engine RESOLVE plugin-side) — terse, never duplicating the projection (R3).
var (
	ProjectDistroConfig  = spec.ProjectDistroConfig
	ProjectBuilderConfig = spec.ProjectBuilderConfig
	ProjectInitConfig    = spec.ProjectInitConfig
)
