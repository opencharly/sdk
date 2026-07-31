package kit

import (
	"os/exec"
	"strings"

	"github.com/opencharly/spec/container"
)

// container_probe.go — the pure container-runtime host probes (K4: relocated from the deleted
// charly/container.go and charly/shell.go — genuinely pure `<engine>` shell-outs with no
// project-loader dependency). Consumed directly by candy/plugin-deploy-pod/plugin-pod/plugin-adb
// and by charly core's remaining caller (commands.go, the check harness), which import kit
// directly (K3 ZERO-ALIASES — no alias file); android_deploy_cmd.go and volume_cp_tags_cmd.go,
// former core callers, are both since deleted.
//
// IsHostNetworked RELOCATED to the spec fabric slice github.com/opencharly/spec/container
// (#55 CHECK-ENGINE cone Option A — a podman-inspect probe, the slice's charter), re-exported below
// so kit.IsHostNetworked call sites are untouched.
var IsHostNetworked = container.IsHostNetworked

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
