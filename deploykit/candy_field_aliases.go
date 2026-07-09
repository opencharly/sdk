package deploykit

// candy_field_aliases.go — deploykit-local bindings onto the sdk types the runtime
// Candy struct + methods carry by short name (so candy.go compiles unchanged after the
// P4 move out of charly/layers.go). PackageSection/TagPkgConfig/RouteConfig/CandyRef are
// deploykit-native (layer_model.go / candy_ref.go), so they need no alias here.

import (
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

type (
	CandyYAML         = spec.Candy
	AliasYAML         = spec.AliasYAML
	CandyCapabilities = spec.CandyCapabilities
	DataYAML          = spec.DataYAML
	ExtractYAML       = spec.ExtractYAML
	HooksConfig       = spec.HooksConfig
	MCPServerYAML     = spec.MCPServerYAML
	PortSpec          = spec.PortSpec
	SecretYAML        = spec.SecretYAML
	SecurityConfig    = spec.SecurityConfig
	ServiceEntry      = spec.ServiceEntry
	VolumeYAML        = spec.VolumeYAML

	EnvConfig = kit.EnvConfig

	CandyArtifact   = vmshared.CandyArtifact
	CandyPluginDecl = vmshared.CandyPluginDecl
	EnvDependency   = vmshared.EnvDependency
	ShellConfig     = vmshared.ShellConfig
	Step            = vmshared.Step
	PackageItem     = vmshared.PackageItem
)

// sortStrings is the in-place sort helper the moved Candy code calls (kit.SortStrings).
var sortStrings = kit.SortStrings
