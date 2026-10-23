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

// BuildStartArgs constructs the container run argument list for a DETACHED service
// (`-d`), for a direct-mode `charly start`.
// rootful selects the engine's mode-specific capability facts (the workload user
// for host-identical bind-mount sharing); pass false for a rootless target.
// entrypoint is the init system command (e.g., ["supervisord", "-n", "-c", "/etc/supervisord.conf"])
// or the fallback (e.g., ["sleep", "infinity"]).
// initDef is the image's baked init contract (ai.opencharly.init_def); it supplies
// the container-runtime argv a systemd-style init declares (systemd mode + stop
// signal). Nil for an image that declares none.
//
// For a systemd-unit ExecStart (which must run in the FOREGROUND so systemd
// supervises the CLI), use BuildForegroundRunArgs — same body, no `-d`.
func BuildStartArgs(engine string, rootful bool, imageRef string, uid, gid int, ports []string, name string, volumes []spec.VolumeMount, bindMounts []ResolvedBindMount, gpu bool, bindAddr string, envVars []string, security spec.SecurityConfig, entrypoint []string, initDef *spec.CapabilityInitDef, workingDir string, network ...string) []string {
	return buildRunArgs(true, engine, rootful, imageRef, uid, gid, ports, name, volumes, bindMounts, gpu, bindAddr, envVars, security, entrypoint, initDef, workingDir, network...)
}

// BuildForegroundRunArgs is BuildStartArgs for a FOREGROUND launch (no `-d`),
// used as a generated systemd unit's ExecStart: the engine CLI stays in the
// foreground for the container's lifetime, so systemd supervises it directly and
// the unit does not go inactive the instant the CLI returns. Shares the ONE argv
// body with BuildStartArgs (R3).
func BuildForegroundRunArgs(engine string, rootful bool, imageRef string, uid, gid int, ports []string, name string, volumes []spec.VolumeMount, bindMounts []ResolvedBindMount, gpu bool, bindAddr string, envVars []string, security spec.SecurityConfig, entrypoint []string, initDef *spec.CapabilityInitDef, workingDir string, network ...string) []string {
	return buildRunArgs(false, engine, rootful, imageRef, uid, gid, ports, name, volumes, bindMounts, gpu, bindAddr, envVars, security, entrypoint, initDef, workingDir, network...)
}

// buildRunArgs is the ONE run-argv builder, detached or foreground. The
// engine-specific bits are capability-driven (kit.EngineCapabilityFor), not
// switched on the engine name:
//   - the CLI binary comes from EngineBinary (podman / docker / nerdctl);
//   - GPU passthrough comes from GPURunArgs (CDI for podman, --gpus for the rest);
//   - `--rm` accompanies `-d` only where the engine allows both (nerdctl REJECTS
//     `-d --rm`, a measured capability fact — NoRemoveWithDetach); a FOREGROUND
//     launch may always use `--rm` (auto-remove on exit);
//   - host-identical bind-mount sharing uses the engine's own keep-id flag when
//     it has one (podman --userns=keep-id:uid=…,gid=…), and otherwise launches the
//     workload as the mode-specific workload user — container-uid 0 for rootless
//     containerd (driven through nerdctl), where container-uid 0 IS the invoking
//     host user in the rootless userns; a rootful launch forces no user.
func buildRunArgs(detach bool, engine string, rootful bool, imageRef string, uid, gid int, ports []string, name string, volumes []spec.VolumeMount, bindMounts []ResolvedBindMount, gpu bool, bindAddr string, envVars []string, security spec.SecurityConfig, entrypoint []string, initDef *spec.CapabilityInitDef, workingDir string, network ...string) []string {
	binary := kit.EngineBinary(engine)
	capability, hasCapability := kit.EngineCapabilityFor(engine)
	args := []string{binary, "run"}
	if detach {
		args = append(args, "-d")
	}
	// `--rm` with `-d`: podman/docker accept both; nerdctl REJECTS `-d --rm`
	// ("flags -d and --rm cannot be specified together", measured live on 2.3.5),
	// so it is dropped for an engine whose NoRemoveWithDetach fact is set. A
	// foreground launch may always `--rm`.
	if !detach || !hasCapability || !capability.NoRemoveWithDetach {
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
		// The workload user is a MODE-SPECIFIC capability fact. WorkloadUserFor
		// selects the ROOTLESS or ROOTFUL field for THIS launch — never assume
		// rootless. Rootless containerd (nerdctl) runs as container-uid 0 because
		// uid 0 IS the invoking host user in the rootless userns; rootful forces
		// no user, so the image's declared user applies.
		workloadUser := kit.WorkloadUserFor(capability, rootful)
		switch {
		case hasCapability && capability.SupportsUsernsKeepID && capability.UsernsKeepIDArg != "":
			// podman: map the invoking user in, so host files stay the caller's.
			args = append(args, fmt.Sprintf("%s:uid=%d,gid=%d", capability.UsernsKeepIDArg, uid, gid))
		case hasCapability && workloadUser != "":
			// no keep-id: run the workload as the mode-specific user and host
			// bind-mount files keep the host uid identity.
			args = append(args, "--user", workloadUser)
		}
	}
	for _, e := range envVars {
		args = append(args, "-e", e)
	}
	// The init's container-runtime mode (systemd mode + stop signal), from the
	// image's baked init contract. One assembly for every engine — the flag
	// spelling is an engine-capability fact, not a switch here.
	args = append(args, kit.InitContainerRuntimeArgs(engine, initDef)...)
	args = append(args, imageRef)
	args = append(args, entrypoint...)
	return args
}
