package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/kit"
)

// container_resolve.go — the deploy-key → running-container resolvers (K4: relocated from the
// deleted charly/container.go and charly/volume_cp_tags_cmd.go). Homed in deploykit (not kit)
// because they need ResolveBoxEngineForDeploy, a deploykit-only mechanism (kit cannot import
// deploykit). The dedup this file's header used to track as incomplete (a 2026-07-20 R1 finding:
// bare core duplicates of ResolveContainer/ResolveSidecarContainer still called by
// check_members.go/cmd.go/etc.) is DONE: both charly/container.go and charly/volume_cp_tags_cmd.go
// are deleted, no bare `func resolveContainer`/`func resolveSidecarContainer` remains anywhere in
// charly core, and every caller (charly's check_members.go/check_endpoint_resolve.go/
// check_venue_resolve.go + candy/plugin-check/plugin-cmd/plugin-pod/plugin-adb/plugin-deploy-pod)
// imports THESE functions directly. This is the ONE shared implementation.

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
