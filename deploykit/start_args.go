package deploykit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
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
func BuildStartArgs(engine, imageRef string, uid, gid int, ports []string, name string, volumes []spec.VolumeMount, bindMounts []ResolvedBindMount, gpu bool, bindAddr string, envVars []string, security spec.SecurityConfig, entrypoint []string, workingDir string, network ...string) []string {
	binary := kit.EngineBinary(engine)
	args := []string{
		binary, "run", "-d", "--rm",
		"--name", name,
		"-w", workingDir,
	}
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
	if engine == "podman" && len(bindMounts) > 0 {
		args = append(args, fmt.Sprintf("--userns=keep-id:uid=%d,gid=%d", uid, gid))
	}
	for _, e := range envVars {
		args = append(args, "-e", e)
	}
	args = append(args, imageRef)
	args = append(args, entrypoint...)
	return args
}
