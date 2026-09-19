package kit

import "github.com/opencharly/spec/container"

// engine.go — re-export of the container-engine helpers, RELOCATED to the spec/container fabric
// slice (#55 fabric-primitive extraction). EngineBinary/GPURunArgs/DetectEngine are host
// engine-resolution primitives (DetectEngine shells `LookPath`), homed in spec/container which
// carries os/exec in its own slice (Rule 2). kit re-exports them here so kit's own callers
// (container_image.go/container_probe.go/runtime_config.go's ResolveRuntime) and every existing
// kit.EngineBinary / kit.GPURunArgs / kit.DetectEngine call site are untouched.
var (
	EngineBinary = container.EngineBinary
	GPURunArgs   = container.GPURunArgs
	DetectEngine = container.DetectEngine

	// EngineCapabilityFor / EngineRunModeFor expose the engine capability DATA
	// (the name→facts table) so kit consumers stop switching on the engine name.
	// EngineCapability is the capability struct type (pod/secret/keep-id/run-mode
	// support), the sdk-visible mirror of the authored #EngineCapability CUE def.
	EngineCapabilityFor = container.EngineCapabilityFor
	EngineRunModeFor    = container.EngineRunModeFor
)

// EngineCapability is the sdk-visible alias of the spec/container capability
// struct — callers write kit.EngineCapability rather than importing the fabric
// slice directly (import-purity, zero aliases in reverse).
type EngineCapability = container.EngineCapability
