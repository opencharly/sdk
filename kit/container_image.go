package kit

import (
	"fmt"
	"os/exec"
	"strings"
)

// container_image.go — the single container→image-ref inspector (K4: relocated from
// charly/commands.go — a genuinely pure podman/docker CLI invocation, no project-loader
// dependency). Used wherever a caller must read what a LIVE container is actually running (mcp
// probes, service init detection, remove hooks, direct-mode start, the pod-lifecycle tunnel
// resolver). Shared between charly core's remaining callers (start.go, commands.go,
// check_endpoint_resolve.go, service.go) and candy/plugin-deploy-pod (pod_lifecycle_resolve.go's
// quadlet-mode move), which now import kit directly (K3 ZERO-ALIASES — no alias file).

// ContainerImageRef returns the image ref backing a running container (.Config.Image via
// `<engine> inspect`). THE single container→image-ref inspector — there is exactly one inspect
// implementation.
func ContainerImageRef(engine, containerName string) (string, error) {
	out, _, exit, err := RunCaptureCmd(exec.Command(EngineBinary(engine), "inspect", "--format", "{{.Config.Image}}", containerName))
	if err != nil {
		return "", fmt.Errorf("inspecting container %s: %w", containerName, err)
	}
	if exit != 0 {
		return "", fmt.Errorf("inspect %s: exit %d", containerName, exit)
	}
	return strings.TrimSpace(out), nil
}

// ContainerImage returns the image ref for a running container, best-effort ("" on error). Thin
// wrapper over ContainerImageRef.
func ContainerImage(engine, containerName string) string {
	ref, _ := ContainerImageRef(engine, containerName)
	return ref
}
