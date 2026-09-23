package deploykit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// start_args.go — the detached-service container-run argv builder (K4 lane B: relocated from
// charly/start.go's buildStartArgs — a genuinely pure argv-building mechanism with no
// project-loader dependency). Homed in deploykit (not kit) because it needs ResolvedBindMount, a
// deploykit-only type. Consumed by candy/plugin-deploy-pod (pod_lifecycle_resolve.go's move, the
// direct-mode `charly start` path); charly core's config_image.go does not call this (it renders
// quadlet units instead), so no core alias is needed for this one.

// BuildStartArgs constructs the container run argument list for a detached service.
// entrypoint is the init system command (e.g., ["supervisord", "-n", "-c", "/etc/supervisord.conf"])
// or the fallback (e.g., ["sleep", "infinity"]).
//
// The engine-specific bits are capability-driven (kit.EngineCapabilityFor), not
// switched on the engine name:
//   - the CLI binary comes from EngineBinary (podman / docker / nerdctl);
//   - GPU passthrough comes from GPURunArgs (CDI for podman, --gpus for the rest);
//   - `--rm` is emitted with `-d` only where the engine allows both (nerdctl
//     REJECTS `-d --rm`, a measured capability fact — NoRemoveWithDetach);
//   - host-identical bind-mount sharing uses the engine's own keep-id flag when
//     it has one (podman --userns=keep-id:uid=…,gid=…), and otherwise launches the
//     workload as WorkloadUser — container-uid 0 for rootless nerdctl, where
//     container-uid 0 IS the invoking host user in the rootless userns.
func BuildStartArgs(engine, imageRef string, uid, gid int, ports []string, name string, volumes []spec.VolumeMount, bindMounts []ResolvedBindMount, gpu bool, bindAddr string, envVars []string, security spec.SecurityConfig, entrypoint []string, workingDir string, network ...string) []string {
	binary := kit.EngineBinary(engine)
	capability, hasCapability := kit.EngineCapabilityFor(engine)
	// `-d` (detached) and `--rm` (auto-remove on exit): podman accepts them
	// together; nerdctl REJECTS `-d --rm` ("flags -d and --rm cannot be specified
	// together", measured live on nerdctl 2.3.5). So `--rm` is emitted only where
	// the engine's capability allows it alongside detach; the container is then
	// cleaned up by the unit's ExecStop (`<engine> stop`) / an explicit rm.
	args := []string{
		binary, "run", "-d",
	}
	if !hasCapability || !capability.NoRemoveWithDetach {
		args = append(args, "--rm")
	}
	args = append(args, "--name", name, "-w", workingDir)
	if len(network) > 0 && network[0] != "" {
		args = append(args, "--network", network[0])
	}
	if gpu {
		args = append(args, kit.GPURunArgs(engine)...)
	}
	args = append(args, SecurityArgs(security)...)
	for _, port := range ports {
		args = append(args, "-p", LocalizePort(port, bindAddr))
	}
	for _, vol := range volumes {
		args = append(args, "-v", fmt.Sprintf("%s:%s", vol.VolumeName, vol.ContainerPath))
	}
	for _, bm := range bindMounts {
		args = append(args, "-v", fmt.Sprintf("%s:%s", bm.HostPath, bm.ContPath))
	}
	for _, m := range security.Mounts {
		if after, ok := strings.CutPrefix(m, "tmpfs:"); ok {
			// tmpfs:/path:options → --tmpfs /path:options
			args = append(args, "--tmpfs", after)
		} else {
			args = append(args, "-v", m)
		}
	}
	if len(bindMounts) > 0 {
		switch {
		case hasCapability && capability.SupportsUsernsKeepID && capability.UsernsKeepIDArg != "":
			// podman: map the invoking user in, so host files stay the caller's.
			args = append(args, fmt.Sprintf("%s:uid=%d,gid=%d", capability.UsernsKeepIDArg, uid, gid))
		case hasCapability && capability.WorkloadUser != "":
			// nerdctl rootless: no keep-id, so run the workload as the user that
			// maps to the invoking host user (container-uid 0) and host bind-mount
			// files keep the host uid identity.
			args = append(args, "--user", capability.WorkloadUser)
		}
	}
	for _, e := range envVars {
		args = append(args, "-e", e)
	}
	args = append(args, imageRef)
	args = append(args, entrypoint...)
	return args
}
