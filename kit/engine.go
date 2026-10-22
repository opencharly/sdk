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

	// EngineCapabilityFor / EngineRunModeFor / IsUnitRunMode expose the engine
	// capability DATA (the name→facts table) so kit consumers stop switching on
	// the engine name. The capability STRUCT TYPE is spec.EngineCapability (the
	// generated schema type) — consumers name it directly; no alias here.
	EngineCapabilityFor = container.EngineCapabilityFor
	// WorkloadUserFor selects the capability's ROOTLESS or ROOTFUL workload-user
	// fact for the launch mode. The two are separate fields because an engine
	// can be driven both ways (containerd), and a consumer that assumes rootless
	// is wrong for the rootful arm.
	WorkloadUserFor  = container.WorkloadUserFor
	EngineRunModeFor = container.EngineRunModeFor
	IsUnitRunMode    = container.IsUnitRunMode
)
