package kit

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestShellExecutorStartProcessPreservesArgvAndPipes(t *testing.T) {
	p, err := (ShellExecutor{}).StartProcess(context.Background(), spec.ProcessLaunch{Argv: []string{"sh", "-c", `IFS= read -r line; printf '%s|%s|%s' "$1" "$line" "$CHARLY_PROCESS_TEST"; printf diagnostic >&2`, "sh", "space $' literal"}, Env: spec.StrMap{"CHARLY_PROCESS_TEST": "environment value"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(p.Stdin(), "payload with spaces\n"); err != nil {
		t.Fatal(err)
	}
	_ = p.Stdin().Close()
	out, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := io.ReadAll(p.Stderr())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "space $' literal|payload with spaces|environment value"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := string(diagnostic); got != "diagnostic" {
		t.Fatalf("stderr = %q", got)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
}

type recordingProcessExecutor struct{ argv []string }

func (r *recordingProcessExecutor) StartProcess(_ context.Context, launch spec.ProcessLaunch) (spec.Process, error) {
	r.argv = append([]string(nil), launch.Argv...)
	return nil, nil
}

type processOnlyDeployExecutor struct {
	spec.DeployExecutor
	*recordingProcessExecutor
}

func TestNestedExecutorStartProcessComposesEveryHopAsArgv(t *testing.T) {
	recorder := &recordingProcessExecutor{}
	root := &processOnlyDeployExecutor{recordingProcessExecutor: recorder}
	container := &NestedExecutor{Parent: root, Jump: NestedJump{Kind: JumpPodmanExec, Target: "box", ExtraArgs: []string{"--env", "A=B C"}}}
	leaf := &NestedExecutor{Parent: container, Jump: NestedJump{Kind: JumpSSH, Target: "agent@inner"}}
	_, err := leaf.StartProcess(context.Background(), spec.ProcessLaunch{Argv: []string{"charly", "__agent-target", "serve", "--stdio"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"podman", "exec", "-i", "--env", "A=B C", "box", "ssh", "-T", "agent@inner", "'charly' '__agent-target' 'serve' '--stdio'"}
	if got := strings.Join(recorder.argv, "\x00"); got != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", recorder.argv, want)
	}
}

func TestNestedExecutorStartProcessRejectsVirshConsole(t *testing.T) {
	root := &processOnlyDeployExecutor{recordingProcessExecutor: &recordingProcessExecutor{}}
	nested := &NestedExecutor{Parent: root, Jump: NestedJump{Kind: JumpVirshConsole, Target: "vm"}}
	if _, err := nested.StartProcess(context.Background(), spec.ProcessLaunch{Argv: []string{"charly"}}); err == nil || !strings.Contains(err.Error(), spec.ErrNotSupported.Error()) {
		t.Fatalf("error = %v, want ErrNotSupported", err)
	}
}
