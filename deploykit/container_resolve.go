package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/kit"
)

// container_resolve.go — the deploy-key → running-container resolvers (K4: relocated from
// charly/container.go and charly/volume_cp_tags_cmd.go). Homed in deploykit (not kit) because they
// need ResolveBoxEngineForDeploy, a deploykit-only mechanism (kit cannot import deploykit). Shared
// between charly core's remaining callers (the check harness, android_deploy_cmd.go, cmd.go) and
// candy/plugin-deploy-pod, which now import deploykit directly (K3 ZERO-ALIASES — no alias file).

// ResolveContainer resolves engine + container name, verifying the container is running.
// Use "." as image name for local mode (returns empty engine and name).
func ResolveContainer(box, instance string) (engine, name string, err error) {
	if box == "." {
		return "", "", nil
	}
	rt, err := kit.ResolveRuntime()
	if err != nil {
		return "", "", err
	}
	boxName := kit.ResolveBoxName(box)
	runEngine := ResolveBoxEngineForDeploy(boxName, instance, rt.RunEngine)
	engine = kit.EngineBinary(runEngine)
	name = kit.ContainerNameInstance(boxName, instance)
	if !kit.ContainerRunning(engine, name) {
		return "", "", fmt.Errorf("container %s is not running", name)
	}
	return engine, name, nil
}

// ResolveSidecarContainer resolves engine + container name for a named sidecar, verifying it is
// running.
func ResolveSidecarContainer(box, instance, sidecar string) (engine, name string, err error) {
	rt, err := kit.ResolveRuntime()
	if err != nil {
		return "", "", err
	}
	boxName := kit.ResolveBoxName(box)
	runEngine := ResolveBoxEngineForDeploy(boxName, instance, rt.RunEngine)
	engine = kit.EngineBinary(runEngine)
	name = kit.SidecarContainerNameInstance(boxName, instance, sidecar)
	if !kit.ContainerRunning(engine, name) {
		return "", "", fmt.Errorf("sidecar container %s is not running", name)
	}
	return engine, name, nil
}
