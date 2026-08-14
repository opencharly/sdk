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
//     would treat a stopped VM as running. RenderQemuArgv always appends
//     `-pidfile <paths.PidFile>` (qemu_render.go), so the absolute path is
//     present in the cmdline of every VM this code starts.
//
// /proc/<pid>/cmdline is NUL-separated, so the flag and its value are two
// adjacent NUL-terminated entries rather than one space-joined string.
//
// Shared by `vm start` (the idempotent already-running guard), `vm list` (the
// state scan), and the preempt arbiter's holder probe — one liveness predicate,
// R3. A weaker copy in any of them means one caller can believe a stopped VM is
// running (or the reverse) while another does not.
func QemuAlive(stateDir string) bool {
	pidFile := filepath.Join(stateDir, "qemu.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
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
