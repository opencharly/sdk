package vmshared

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// nulJoin builds a /proc/<pid>/cmdline-shaped buffer: NUL-separated argv.
func nulJoin(args ...string) []byte {
	var out []byte
	for _, a := range args {
		out = append(out, []byte(a)...)
		out = append(out, 0)
	}
	return out
}

func TestQemuCmdlineOwnsPidfile(t *testing.T) {
	const mine = "/home/u/.local/share/charly/vm/box-vm/qemu.pid"
	const other = "/home/u/.local/share/charly/vm/other-vm/qemu.pid"

	cases := []struct {
		name    string
		cmdline []byte
		want    bool
	}{
		{
			name:    "this vm's qemu",
			cmdline: nulJoin("/usr/bin/qemu-system-x86_64", "-m", "4G", "-pidfile", mine),
			want:    true,
		},
		{
			// The case a bare "qemu-system" substring check gets WRONG: a PID
			// recycled onto a DIFFERENT VM's qemu. Without the pidfile match the
			// caller would treat this VM as running when it is stopped.
			name:    "another vm's qemu on a recycled pid",
			cmdline: nulJoin("/usr/bin/qemu-system-x86_64", "-m", "4G", "-pidfile", other),
			want:    false,
		},
		{
			// The case a bare Signal(0) probe gets wrong.
			name:    "pid recycled onto a non-qemu process",
			cmdline: nulJoin("/usr/bin/bash", "-c", "sleep 600"),
			want:    false,
		},
		{
			name:    "qemu with no pidfile flag",
			cmdline: nulJoin("/usr/bin/qemu-system-aarch64", "-m", "2G"),
			want:    false,
		},
		{
			// -pidfile present but the value belongs to nothing we started.
			name:    "non-qemu process carrying a -pidfile flag",
			cmdline: nulJoin("/usr/bin/some-daemon", "-pidfile", mine),
			want:    false,
		},
		{
			name:    "trailing -pidfile with no value",
			cmdline: nulJoin("/usr/bin/qemu-system-x86_64", "-pidfile"),
			want:    false,
		},
		{
			name:    "empty cmdline",
			cmdline: nil,
			want:    false,
		},
		{
			// A path that merely CONTAINS the target as a prefix must not match;
			// the comparison is whole-field, not substring.
			name:    "pidfile path is a prefix of a longer path",
			cmdline: nulJoin("/usr/bin/qemu-system-x86_64", "-pidfile", mine+".bak"),
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := qemuCmdlineOwnsPidfile(tc.cmdline, mine); got != tc.want {
				t.Fatalf("qemuCmdlineOwnsPidfile() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQemuAliveMissingPidfile(t *testing.T) {
	// A state dir with no qemu.pid is not alive — and must not panic.
	if QemuAlive(t.TempDir()) {
		t.Fatal("QemuAlive() = true for a state dir with no qemu.pid")
	}
}

func TestQemuAliveUnparseablePidfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "qemu.pid"), []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if QemuAlive(dir) {
		t.Fatal("QemuAlive() = true for an unparseable qemu.pid")
	}
}

// TestQemuAliveLiveForeignQemu drives the WHOLE predicate against a real
// running process — the pidfile read, the integer parse, FindProcess, the
// Signal(0) probe, and the /proc/<pid>/cmdline read all execute for real, and
// only the ownership match decides the answer.
//
// It is the recycled-PID case made executable, and it is built to be
// PROBATIVE: the helper is launched through a symlink named `qemu-system-…`,
// so its cmdline genuinely contains "qemu-system" while it owns no pidfile at
// all. Both weaker predicates therefore report it RUNNING — a bare Signal(0)
// because the PID is alive, and Signal(0)+substring because the cmdline
// matches. Only whole-field `-pidfile` ownership rejects it.
//
// Verified by mutation: swapping the final check for
// `bytes.Contains(cmdline, []byte("qemu-system"))` makes this test FAIL. An
// earlier version of it used a plain `sleep` helper and did NOT fail under
// that mutation — it was passing for the wrong reason, because `sleep` has no
// "qemu-system" in its cmdline either way.
//
// The real-world stake: a VM whose PID was recycled onto another VM's qemu
// would be reported running, and `vm start` would skip starting it.
func TestQemuAliveLiveForeignQemu(t *testing.T) {
	dir := t.TempDir()

	// A helper whose argv[0] contains "qemu-system" but which is not our qemu.
	fakeQemu := filepath.Join(dir, "qemu-system-fake")
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary to build a helper from: %v", err)
	}
	if err := os.Symlink(sleepBin, fakeQemu); err != nil {
		t.Skipf("cannot create the helper symlink: %v", err)
	}

	cmd := exec.Command(fakeQemu, "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn the helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	pidData := []byte(strconv.Itoa(cmd.Process.Pid) + "\n")
	if err := os.WriteFile(filepath.Join(dir, "qemu.pid"), pidData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Guard both premises — a test that passes because the process died, or
	// because its cmdline lacks "qemu-system", proves nothing.
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("helper is not signalable, premise broken: %v", err)
	}
	// cmd.Start returns once the fork succeeds; the child's /proc cmdline is
	// only populated after its exec completes, so a read here can legitimately
	// come back empty. Poll briefly rather than racing it — an empty read would
	// otherwise look like a broken premise.
	var cmdline []byte
	for i := 0; i < 200; i++ {
		cmdline, err = os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", cmd.Process.Pid))
		if err != nil {
			t.Skipf("cannot read /proc cmdline (no procfs?): %v", err)
		}
		if bytes.Contains(cmdline, []byte("qemu-system")) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(cmdline, []byte("qemu-system")) {
		t.Fatalf("helper cmdline never showed %q, premise broken: %q", "qemu-system", cmdline)
	}

	if QemuAlive(dir) {
		t.Fatal("QemuAlive() = true for a LIVE qemu-named process that does not own " +
			"this pidfile — the predicate degraded to a substring scan")
	}
}
