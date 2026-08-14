package vmshared

import "testing"

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
