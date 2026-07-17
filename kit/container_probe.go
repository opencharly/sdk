package kit

import (
	"os/exec"
	"strings"
)

// container_probe.go — the pure container-runtime host probes (K4: relocated from
// charly/container.go and charly/shell.go — genuinely pure `<engine>` shell-outs with no
// project-loader dependency). Consumed directly by candy/plugin-deploy-pod and by charly core's
// remaining callers (the check harness, android_deploy_cmd.go, volume_cp_tags_cmd.go), which now
// import kit directly (K3 ZERO-ALIASES — no alias file).

// ContainerRunning reports whether a container is running. Package-level var for testability
// (tests inject a stub, same pattern as EnsureCharlyNetwork/InspectLabels).
var ContainerRunning = defaultContainerRunning

func defaultContainerRunning(engine, name string) bool {
	binary := EngineBinary(engine)
	cmd := exec.Command(binary, "container", "inspect",
		"--format", "{{.State.Running}}", name)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// IsHostNetworked checks if a running container uses --network host.
func IsHostNetworked(engine, containerName string) bool {
	cmd := exec.Command(engine, "inspect", "--format",
		"{{.HostConfig.NetworkMode}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "host"
}
