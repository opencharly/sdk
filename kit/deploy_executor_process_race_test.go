package kit

import (
	"context"
	"io"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestStartProcessWaitMonitorNeverClosesConsumerPipes is the deterministic
// regression gate for the Wait/pipe-ownership race: with os/exec's own
// StdoutPipe/StderrPipe, the Wait monitor closed the pipes the consumer was
// still reading ("incorrect to call Wait before all reads from the pipe have
// completed" — os/exec), surfacing as `read |0: file already closed` roughly
// one run in ten on the small-payload StartProcess tests. A payload larger
// than the pipe buffer forces ReadAll to span several read syscalls, so the
// Wait monitor ALWAYS fires mid-drain — the race window is open on every
// iteration, not by scheduler luck. The os.Pipe ownership pattern (the pipes
// belong to the caller; Wait only closes pipes it created itself) makes the
// monitor harmless.
func TestStartProcessWaitMonitorNeverClosesConsumerPipes(t *testing.T) {
	for i := range 8 {
		p, err := (ShellExecutor{}).StartProcess(context.Background(), spec.ProcessLaunch{
			Argv: []string{"sh", "-c", "head -c 1048576 /dev/zero; head -c 65536 /dev/zero >&2"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(p.Stdin(), ""); err != nil {
			t.Fatalf("iteration %d: stdin: %v", i, err)
		}
		_ = p.Stdin().Close()
		out, err := io.ReadAll(p.Stdout())
		if err != nil {
			t.Fatalf("iteration %d: stdout read: %v", i, err)
		}
		diagnostic, err := io.ReadAll(p.Stderr())
		if err != nil {
			t.Fatalf("iteration %d: stderr read: %v", i, err)
		}
		if err := p.Wait(); err != nil {
			t.Fatalf("iteration %d: wait: %v", i, err)
		}
		if len(out) != 1048576 {
			t.Fatalf("iteration %d: stdout = %d bytes, want 1048576", i, len(out))
		}
		if len(diagnostic) != 65536 {
			t.Fatalf("iteration %d: stderr = %d bytes, want 65536", i, len(diagnostic))
		}
		if err := p.Close(); err != nil {
			t.Fatalf("iteration %d: idempotent close: %v", i, err)
		}
	}
}
