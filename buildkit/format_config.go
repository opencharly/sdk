package buildkit

import (
	"github.com/opencharly/spec/spec"
)

// Local aliases onto the CUE-sourced spec build-vocabulary config types, so
// buildkit's build-engine code (config_resolve.go / resolved_box.go /
// system_packages.go) reads unchanged without importing charly core. #55 step
// 3-III relocated the DistroConfig/BuilderConfig CONTAINERS + their
// vocabulary-resolution methods (ResolveDistro / ResolveInherits /
// AllFormatNames / FindFormat / ValidFormat / ExpandPackageInheritance /
// ValidBuilderType / BuilderNames) and the WrapDistroDef / DistroTagChain free
// functions to spec (schema/{distro,builder}.cue + spec/{distro,builder}_config_methods.go),
// so every charly/plugin file that uses ONLY these value types drops its
// sdk/buildkit import for spec — the import-purity lever. buildkit keeps these
// thin aliases exactly as it does for DistroDef/BuilderDef/BuilderMap.
type (
	DistroDef     = spec.ResolvedDistro
	FormatDef     = spec.Format
	BuilderDef    = spec.Builder
	DistroConfig  = spec.DistroConfig
	BuilderConfig = spec.BuilderConfig
)
