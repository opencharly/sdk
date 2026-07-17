package deploykit

import (
	"strconv"
	"strings"

	"github.com/opencharly/sdk/spec"
)

// security.go — the pure security-argv-building helpers (K4 lane B: relocated from
// charly/security.go), PLUS the candy-merge logic (W9: the CollectSecurity split). The prior header
// here read "CollectSecurity... STAYS in charly core" — that was true of the function AS A WHOLE, but
// conflated two genuinely separate concerns: WHICH candies compose a box (a *Config/candy-tree walk —
// box-resolution, genuinely core) and HOW their security configs merge (a pure candy-order fold with
// zero *Config coupling). MergeCandySecurity below is the second half, so charly's CollectSecurity
// (security.go) becomes a thin host wrapper: resolve the box's candy order (unchanged, still needs
// *Config), then call this pure merge over the CandyModel interface — matching the split every other
// build-render/OCI-label-collector R-item in this file already follows (SecurityArgs/ResourceCapArgs).
// SecurityArgs/ResourceCapArgs are the pure, side-effect-free argv builders three charly call sites
// (config_image.go, start.go, pod_lifecycle_resolve.go) call DIRECTLY as deploykit.SecurityArgs — R1
// fix: this header previously claimed a "security_aliases.go passthrough" that never existed; charly
// carried its own byte-identical duplicate instead (deleted in the same change as the CollectSecurity
// split above). IpcModeBlocksShmSize already lives here (quadlet.go).

// MergeCandySecurity folds a box's ordered candy security configs into one SecurityConfig, then
// applies the box-level override — the pure half of charly's CollectSecurity (candy_chain resolution
// + the *Config/BoxConfig lookup stay host-side; this loop is the R-item every OCI-label-collector
// build-render consumer — host TODAY, an out-of-process build/deploy plugin tomorrow — can share).
// Merge rules (unchanged from the pre-split charly/security.go): if any candy sets privileged: true,
// the result is privileged; cap_add/devices/security_opt/group_add/mounts are unioned; shm_size takes
// the largest value (biggest-wins — more shared memory is safer); memory_max/memory_high/
// memory_swap_max/cpus take the smallest (smallest-wins — a tighter cap is a smaller blast radius).
// Image-level override replaces rather than merges (last writer, authored intent wins).
func MergeCandySecurity(candies []CandyModel, override *SecurityConfig) SecurityConfig {
	var merged SecurityConfig
	for _, c := range candies {
		if c == nil {
			continue
		}
		sec := c.Security()
		if sec == nil {
			continue
		}
		if sec.Privileged {
			merged.Privileged = true
		}
		if sec.CgroupNS != "" {
			merged.CgroupNS = sec.CgroupNS
		}
		if sec.IpcMode != "" {
			merged.IpcMode = sec.IpcMode
		}
		merged.CapAdd = AppendUnique(merged.CapAdd, sec.CapAdd...)
		merged.Devices = AppendUnique(merged.Devices, sec.Devices...)
		merged.SecurityOpt = AppendUnique(merged.SecurityOpt, sec.SecurityOpt...)
		merged.GroupAdd = AppendUnique(merged.GroupAdd, sec.GroupAdd...)
		merged.Mounts = AppendUnique(merged.Mounts, sec.Mounts...)
		if sec.ShmSize != "" {
			merged.ShmSize = maxShmSize(merged.ShmSize, sec.ShmSize)
		}
		if sec.MemoryMax != "" {
			merged.MemoryMax = minCap(merged.MemoryMax, sec.MemoryMax)
		}
		if sec.MemoryHigh != "" {
			merged.MemoryHigh = minCap(merged.MemoryHigh, sec.MemoryHigh)
		}
		if sec.MemorySwapMax != "" {
			merged.MemorySwapMax = minCap(merged.MemorySwapMax, sec.MemorySwapMax)
		}
		if sec.Cpus != "" {
			merged.Cpus = minCpus(merged.Cpus, sec.Cpus)
		}
	}
	if override == nil {
		return merged
	}
	merged.Privileged = override.Privileged
	if override.CgroupNS != "" {
		merged.CgroupNS = override.CgroupNS
	}
	if override.IpcMode != "" {
		merged.IpcMode = override.IpcMode
	}
	if len(override.CapAdd) > 0 {
		merged.CapAdd = AppendUnique(merged.CapAdd, override.CapAdd...)
	}
	if len(override.Devices) > 0 {
		merged.Devices = AppendUnique(merged.Devices, override.Devices...)
	}
	if len(override.SecurityOpt) > 0 {
		merged.SecurityOpt = AppendUnique(merged.SecurityOpt, override.SecurityOpt...)
	}
	if override.ShmSize != "" {
		merged.ShmSize = override.ShmSize
	}
	if len(override.GroupAdd) > 0 {
		merged.GroupAdd = AppendUnique(merged.GroupAdd, override.GroupAdd...)
	}
	if len(override.Mounts) > 0 {
		merged.Mounts = AppendUnique(merged.Mounts, override.Mounts...)
	}
	if override.MemoryMax != "" {
		merged.MemoryMax = override.MemoryMax
	}
	if override.MemoryHigh != "" {
		merged.MemoryHigh = override.MemoryHigh
	}
	if override.MemorySwapMax != "" {
		merged.MemorySwapMax = override.MemorySwapMax
	}
	if override.Cpus != "" {
		merged.Cpus = override.Cpus
	}
	return merged
}

// parseShmBytes parses a size string like "256m", "1g", "1024" into bytes.
func parseShmBytes(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "g"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "m"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "k"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "k")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n * multiplier
}

// maxShmSize returns the larger of two shm size strings.
func maxShmSize(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if parseShmBytes(a) >= parseShmBytes(b) {
		return a
	}
	return b
}

// minCap returns the smaller (tighter) of two size-cap strings for memory
// limits — smallest wins because a tighter cap is a smaller blast radius.
// This is the opposite of maxShmSize, which picks the larger shm_size.
func minCap(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if parseShmBytes(a) <= parseShmBytes(b) {
		return a
	}
	return b
}

// minCpus returns the smaller (tighter) of two CPU-quota strings like "2.5".
// Strings that fail to parse are treated as unlimited so the other side wins.
func minCpus(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	av, aerr := strconv.ParseFloat(strings.TrimSpace(a), 64)
	bv, berr := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if aerr != nil {
		return b
	}
	if berr != nil {
		return a
	}
	if av <= bv {
		return a
	}
	return b
}

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
