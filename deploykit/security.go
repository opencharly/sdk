package deploykit

import (
	"github.com/opencharly/sdk/spec"
)

// security.go — the pure security-argv-building helpers (K4 lane B: relocated from
// charly/security.go). CollectSecurity (the candy-merge logic, needs *Config/*Candy — core
// config-resolution types — plus its own maxShmSize/minCap/minCpus/parseShmBytes helpers) STAYS in
// charly core; SecurityArgs/ResourceCapArgs are the pure, side-effect-free argv builders shared
// between charly's config_image.go (group 3, not moving yet) and candy/plugin-deploy-pod
// (pod_lifecycle_resolve.go's move) via the security_aliases.go passthrough. IpcModeBlocksShmSize
// already lives here (quadlet.go).

// SecurityArgs returns the container run arguments for the given security config.
//
// Note on the ShmSize+IpcMode interaction: podman rejects `--shm-size`
// when the IPC namespace is shared with the host (`--ipc=host`)
// because the host's /dev/shm is shared in-kernel and sized by the
// host kernel; an explicit shm-size on the container makes no sense
// in that case and yields a runtime error like "cannot set shmsize
// when running in the {host} IPC Namespace". Same logic applies to
// the quadlet generator's ShmSize= directive elsewhere.
func SecurityArgs(sec spec.SecurityConfig) []string {
	emitShmSize := sec.ShmSize != "" && !IpcModeBlocksShmSize(sec.IpcMode)
	if sec.Privileged {
		args := []string{"--privileged"}
		// Pass security_opt even when privileged — nested containers need
		// explicit label=disable and seccomp=unconfined since --privileged
		// alone doesn't propagate through container nesting levels.
		for _, opt := range sec.SecurityOpt {
			args = append(args, "--security-opt", opt)
		}
		if sec.CgroupNS != "" {
			args = append(args, "--cgroupns", sec.CgroupNS)
		}
		if sec.IpcMode != "" {
			args = append(args, "--ipc", sec.IpcMode)
		}
		if emitShmSize {
			args = append(args, "--shm-size", sec.ShmSize)
		}
		args = append(args, ResourceCapArgs(sec)...)
		return args
	}
	var args []string
	for _, cap := range sec.CapAdd {
		args = append(args, "--cap-add", cap)
	}
	for _, dev := range sec.Devices {
		args = append(args, "--device", dev)
	}
	for _, opt := range sec.SecurityOpt {
		args = append(args, "--security-opt", opt)
	}
	for _, group := range sec.GroupAdd {
		args = append(args, "--group-add", group)
	}
	if sec.CgroupNS != "" {
		args = append(args, "--cgroupns", sec.CgroupNS)
	}
	if sec.IpcMode != "" {
		args = append(args, "--ipc", sec.IpcMode)
	}
	if emitShmSize {
		args = append(args, "--shm-size", sec.ShmSize)
	}
	args = append(args, ResourceCapArgs(sec)...)
	return args
}

// ResourceCapArgs returns the podman run flags for memory and CPU caps.
// Emitted identically in both the privileged and non-privileged branches
// of SecurityArgs because privileged containers still need resource limits.
func ResourceCapArgs(sec spec.SecurityConfig) []string {
	var args []string
	if sec.MemoryMax != "" {
		args = append(args, "--memory", sec.MemoryMax)
	}
	if sec.MemoryHigh != "" {
		args = append(args, "--memory-reservation", sec.MemoryHigh)
	}
	if sec.MemorySwapMax != "" {
		args = append(args, "--memory-swap", sec.MemorySwapMax)
	}
	if sec.Cpus != "" {
		args = append(args, "--cpus", sec.Cpus)
	}
	return args
}
