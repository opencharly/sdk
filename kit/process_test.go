package kit

import (
	"bufio"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestSortedEnvPairs(t *testing.T) {
	got := SortedEnvPairs(map[string]string{"B": "two words", "A": "1", "C": ""})
	want := []string{"A=1", "B=two words", "C="}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("SortedEnvPairs = %#v, want %#v", got, want)
	}
	if out := SortedEnvPairs(nil); len(out) != 0 {
		t.Fatalf("SortedEnvPairs(nil) = %#v, want empty", out)
	}
}

func TestRemoteLaunchCommand(t *testing.T) {
	tests := []struct {
		name   string
		launch spec.ProcessLaunch
		want   string
	}{
		{
			name:   "argv only",
			launch: spec.ProcessLaunch{Argv: []string{"charly", "__agent-target", "serve", "--stdio"}},
			want:   "'charly' '__agent-target' 'serve' '--stdio'",
		},
		{
			name: "working dir and env stay target-side",
			launch: spec.ProcessLaunch{
				Argv:       []string{"run", "a b"},
				WorkingDir: "/work dir",
				Env:        map[string]string{"TOKEN": "a b'$", "A": "1"},
			},
			want: `cd '/work dir' && env 'A=1' 'TOKEN=a b'\''$' 'run' 'a b'`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RemoteLaunchCommand(tc.launch); got != tc.want {
				t.Fatalf("RemoteLaunchCommand = %q, want %q", got, tc.want)
			}
		})
	}
}

// startTrapChild launches a Setpgid child through the same caller-owned pipe
// machinery the executors use and blocks until the child reports its signal
// traps are installed. The readiness line is the synchronization point:
// without it, a shutdown signal could arrive before the trap exists and the
// test would assert on the default disposition instead of the trap.
func startTrapChild(t *testing.T, script string) (*exec.Cmd, processPipes, *bufio.Reader, chan struct{}) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pipes, err := startProcessPipes(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = pipes.stdin.Close()
		_ = pipes.stdout.Close()
		_ = pipes.stderr.Close()
	})
	reader := bufio.NewReader(pipes.stdout)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child readiness = %q, %v", line, err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return cmd, pipes, reader, done
}

func TestShutdownProcessGroupTerminatesBeforeKilling(t *testing.T) {
	// A child stopped by the SIGTERM stage prints from its trap; a child hit by
	// SIGKILL first could never print.
	cmd, pipes, reader, done := startTrapChild(t, `trap 'echo caught-term; exit 0' TERM; echo ready; sleep 3600 & wait`)
	ShutdownProcessGroup(cmd, pipes.stdin, done)
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), "caught-term") {
		t.Fatalf("child did not exit through its SIGTERM trap: %q", rest)
	}
}

func TestShutdownProcessGroupEscalatesToKill(t *testing.T) {
	// This child never reads stdin and ignores SIGTERM, so only the SIGKILL
	// escalation can reap it. ShutdownProcessGroup returning at all is the
	// proof: without the escalation the call blocks forever on an immortal
	// child.
	cmd, pipes, _, done := startTrapChild(t, `trap '' TERM; echo ready; while :; do sleep 60; done`)
	ShutdownProcessGroup(cmd, pipes.stdin, done)
}
