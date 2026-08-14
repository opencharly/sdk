package vmshared

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// qemuProcFromStateDir resolves <stateDir>/qemu.pid to the process it names,
// returning that process, the pidfile path, and whether resolution succeeded.
// It asserts only that the PID parsed — NOT that the process is alive or is a
// qemu; callers add the checks their question needs (QemuAlive probes liveness
// and pidfile ownership, KillQemuByPID just kills).
//
// One resolution step for the package (R3): the read-parse-find sequence was
// hand-rolled per call site, which put the "qemu.pid" literal in two places
// and duplicated the same three error guards.
func qemuProcFromStateDir(stateDir string) (*os.Process, string, bool) {
	pidFile := filepath.Join(stateDir, "qemu.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return nil, pidFile, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, pidFile, false
	}
	// os.FindProcess never returns a non-nil error on Unix, but the signature
	// is portable — handled once here rather than at every call site.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, pidFile, false
	}
	return proc, pidFile, true
}

// QemuAlive reports whether the QEMU process recorded in <stateDir>/qemu.pid is
// still running AND is the process that pidfile belongs to.
//
// A pidfile alone is not liveness, and neither is a bare Signal(0) probe: PIDs
// are recycled, so a dead VM's stale pidfile can name a PID that now belongs to
// something else entirely. Two checks close that:
//
//  1. Signal(0) — the PID exists and is signalable.
//  2. /proc/<pid>/cmdline contains "-pidfile <stateDir>/qemu.pid" — the running
//     process is not merely SOME qemu, it is the qemu started for THIS state
//     dir. Matching the bare "qemu-system" substring is not enough: a PID
//     recycled onto a DIFFERENT VM's qemu would satisfy it, and the caller
//     would treat a stopped VM as running.
//
// The second check rests on a CALLER invariant, not a renderer guarantee, and
// the distinction matters to anyone adding a launch path. RenderQemuArgv
// appends `-pidfile` CONDITIONALLY — `if paths.PidFile != ""` (qemu_render.go)
// — so an empty PidFile is explicitly permitted there. What makes the argv
// reliable is that every production launch sets it: VmRuntimePaths.PidFile is
// always `<vmStateDir>/qemu.pid`, byte-identical to what this function
// computes, and `vm start` relaunches from the persisted command file rendered
// with that same argv. A future caller that leaves PidFile empty would make
// QemuAlive report its VMs DEAD — the worse failure direction, since callers
// act on "not running" by starting a second qemu. Set PidFile, or teach this
// predicate the new launch shape.
//
// /proc/<pid>/cmdline is NUL-separated, so the flag and its value are two
// adjacent NUL-terminated entries rather than one space-joined string.
//
// Shared by `vm start` (the idempotent already-running guard), `vm list` (the
// state scan), and the preempt arbiter's holder probe — one liveness predicate,
// R3. A weaker copy in any of them means one caller can believe a stopped VM is
// running (or the reverse) while another does not.
func QemuAlive(stateDir string) bool {
	proc, pidFile, ok := qemuProcFromStateDir(stateDir)
	if !ok {
		return false
	}
	pid := proc.Pid
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return qemuCmdlineOwnsPidfile(cmdline, pidFile)
}

// qemuCmdlineOwnsPidfile reports whether a NUL-separated /proc/<pid>/cmdline is
// a qemu-system invocation carrying `-pidfile <pidFile>`. Split out so the
// matching is unit-testable without a live process.
func qemuCmdlineOwnsPidfile(cmdline []byte, pidFile string) bool {
	fields := bytes.Split(cmdline, []byte{0})
	hasQemu, ownsPidfile := false, false
	for i, f := range fields {
		if bytes.Contains(f, []byte("qemu-system")) {
			hasQemu = true
		}
		if string(f) == "-pidfile" && i+1 < len(fields) && string(fields[i+1]) == pidFile {
			ownsPidfile = true
		}
	}
	return hasQemu && ownsPidfile
}
