package kit

// engine.go — pure container-engine helpers (EngineBinary/GPURunArgs) moved to kit
// in P4. The Config/loader-coupled engine resolvers (ResolveBoxEngine*, ImageRuntime)
// stay in charly/engine.go.

func EngineBinary(engine string) string {
	switch engine {
	case "podman":
		return "podman"
	case "auto":
		if detected, err := DetectEngine(); err == nil {
			return detected
		}
		return "docker"
	default:
		return "docker"
	}
}

func GPURunArgs(engine string) []string {
	switch engine {
	case "podman":
		return []string{"--device", "nvidia.com/gpu=all"}
	default:
		return []string{"--gpus", "all"}
	}
}
