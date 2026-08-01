package kit

import (
	"github.com/opencharly/spec/spec"
)

// quadlet_paths.go — the on-disk quadlet/systemd path helpers + host existence probes
// (QuadletDir / SystemdUserDir / QuadletExists[Instance]) AND the pure quadlet/service FILENAME
// funcs ALL live in spec/spec (quadlet_paths.go + quadlet_names.go, #55 value extraction + coneD
// import-purity); sdk/kit re-exports them so existing kit.QuadletDir / kit.QuadletFilename /
// kit.ServiceName… call sites are untouched. New charly-core consumers reference spec.* directly
// (charly core imports ONLY spec, never sdk/kit).

var (
	ServiceName                    = spec.ServiceName
	ServiceNameInstance            = spec.ServiceNameInstance
	QuadletFilename                = spec.QuadletFilename
	QuadletFilenameInstance        = spec.QuadletFilenameInstance
	PodQuadletFilename             = spec.PodQuadletFilename
	PodQuadletFilenameInstance     = spec.PodQuadletFilenameInstance
	SidecarQuadletFilename         = spec.SidecarQuadletFilename
	SidecarQuadletFilenameInstance = spec.SidecarQuadletFilenameInstance

	QuadletDir            = spec.QuadletDir
	SystemdUserDir        = spec.SystemdUserDir
	QuadletExists         = spec.QuadletExists
	QuadletExistsInstance = spec.QuadletExistsInstance
)
