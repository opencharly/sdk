package kit

import "github.com/opencharly/spec/spec"

// container_name.go — re-export of the deterministic container-naming mechanism, RELOCATED to
// spec (#55 value extraction, spec/spec/container_name.go — pure kind-blind string formatting).
// kit re-exports so every existing kit.ContainerName / kit.ContainerNameInstance call site
// (charly core, sdk/deploykit, the candies) is untouched. New consumers should reference spec.*
// directly. See /charly-core:deploy.

var (
	ContainerName         = spec.ContainerName
	ContainerNameInstance = spec.ContainerNameInstance
)
