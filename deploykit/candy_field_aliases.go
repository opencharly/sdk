package deploykit

// candy_field_aliases.go — deploykit-local bindings onto the sdk types the runtime
// Candy struct + methods carry by short name (so candy.go compiles unchanged after the
// P4 move out of charly/layers.go). PackageSection/TagPkgConfig/RouteConfig/CandyRef are
// deploykit-native (layer_model.go / candy_ref.go), so they need no alias here.

import (
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

type (
	CandyYAML = spec.Candy
	// AliasYAML is the RESOLVED twin, not the authored shape: the candy sub-shapes are one def
	// wearing two hats (authored -> the plugin per 3.1; wire -> spec per 10.2), and a reader only
	// ever sees a body that has already been validated. This is the split the vm unit landed
	// (VmSpec = ResolvedVm), and what lets the authored defs leave spec without taking their
	// decode target with them.
	AliasYAML         = spec.ResolvedCandyAlias
	CandyCapabilities = spec.CandyCapabilities
	DataYAML          = spec.DataYAML
	ExtractYAML       = spec.ExtractYAML
	HooksConfig       = spec.HooksConfig
	MCPServerYAML     = spec.MCPServerYAML
	PortSpec          = spec.PortSpec
	SecretYAML        = spec.SecretYAML
	SecurityConfig    = spec.SecurityConfig
	ServiceEntry      = spec.ServiceEntry
	VolumeYAML        = spec.ResolvedCandyVolume

	EnvConfig = kit.EnvConfig

	CandyArtifact   = vmshared.CandyArtifact
	CandyPluginDecl = vmshared.CandyPluginDecl
	EnvDependency   = vmshared.EnvDependency
	ShellConfig     = vmshared.ShellConfig
	Step            = vmshared.Step
	PackageItem     = vmshared.PackageItem
)
