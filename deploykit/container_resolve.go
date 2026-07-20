package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/kit"
)

// container_resolve.go — the deploy-key → running-container resolvers (K4: relocated from
// charly/container.go and charly/volume_cp_tags_cmd.go). Homed in deploykit (not kit) because they
// need ResolveBoxEngineForDeploy, a deploykit-only mechanism (kit cannot import deploykit).
//
// CURRENT STATE (corrected 2026-07-20, DEPLOY-wave R1 finding): charly/container.go's
// resolveContainer and charly/volume_cp_tags_cmd.go's resolveSidecarContainer are STILL bare,
// undeleted duplicates of ResolveContainer/ResolveSidecarContainer below — an incomplete cutover,
// not a completed one. The bare core versions remain the ones actually called by
// check_members.go, check_endpoint_resolve.go, cmd.go, check_venue.go, check_cmd.go,
// host_build_check_run.go, pod_lifecycle_resolve.go, and android_deploy_cmd.go (verified by grep,
// not assumed) — every one of those is CHECK-wave or android-wave territory, so the dedup sweep
// (repoint each caller to deploykit.ResolveContainer/.ResolveSidecarContainer, delete the core
// duplicates) is tracked to the CHECK wave's inventory, not done here. candy/plugin-pod (the
// DEPLOY wave's CLI-struct port) is the one confirmed consumer that imports these deploykit
// functions directly today (its VolumeCmd/CpCmd leaves, K3 ZERO-ALIASES — no alias file).

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
