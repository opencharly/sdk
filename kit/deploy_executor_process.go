package kit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/opencharly/sdk/spec"
)

const processCloseGrace = 2 * time.Second

// commandProcess is the shared lifecycle implementation for exact-argv
// processes started by ShellExecutor, SSHExecutor, and NestedExecutor.
type commandProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	done   chan struct{}

	mu      sync.Mutex
	waitErr error
	close   sync.Once
}

func startCommandProcess(cmd *exec.Cmd) (spec.Process, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("process stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("process stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("process stderr: %w", err)
	}
	bindProcessGroupKill(cmd)
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	p := &commandProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(p.done)
	}()
	return p, nil
}

func (p *commandProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *commandProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *commandProcess) Stderr() io.ReadCloser { return p.stderr }

func (p *commandProcess) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *commandProcess) Close() error {
	p.close.Do(func() {
		_ = p.stdin.Close()
		select {
		case <-p.done:
		case <-time.After(processCloseGrace):
			if p.cmd.Process != nil {
				_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			}
			<-p.done
		}
	})
	err := p.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil
	}
	return err
}

func validateProcessLaunch(launch spec.ProcessLaunch) error {
	argv := launch.Argv
	if len(argv) == 0 || argv[0] == "" {
		return errors.New("process argv must name an executable")
	}
	for i, arg := range argv {
		if containsNUL(arg) {
			return fmt.Errorf("process argv[%d] contains NUL", i)
		}
	}
	for key := range launch.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return fmt.Errorf("process environment key %q is invalid", key)
		}
	}
	return nil
}

func containsNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}

// StartProcess launches argv directly on the local venue. No shell is involved.
func (ShellExecutor) StartProcess(ctx context.Context, launch spec.ProcessLaunch) (spec.Process, error) {
	if err := validateProcessLaunch(launch); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, launch.Argv[0], launch.Argv[1:]...)
	cmd.Dir = launch.WorkingDir
	cmd.Env = append(os.Environ(), processEnvPairs(launch.Env)...)
	return startCommandProcess(cmd)
}

// StartProcess launches one exact argv through SSH. OpenSSH necessarily sends a
// remote command string; POSIX single quoting each token preserves the argv
// boundary and prevents interpolation by the remote login shell.
func (e *SSHExecutor) StartProcess(ctx context.Context, launch spec.ProcessLaunch) (spec.Process, error) {
	if err := validateProcessLaunch(launch); err != nil {
		return nil, err
	}
	args := e.SSHBaseArgs()
	remote := remoteLaunchCommand(launch)
	args = append(args, remote)
	return startCommandProcess(exec.CommandContext(ctx, "ssh", args...))
}

func remoteLaunchCommand(launch spec.ProcessLaunch) string {
	remote := ""
	if launch.WorkingDir != "" {
		remote = "cd " + ShellQuote(launch.WorkingDir) + " && "
	}
	if len(launch.Env) > 0 {
		remote += "env"
		for _, pair := range processEnvPairs(launch.Env) {
			remote += " " + ShellQuote(pair)
		}
		remote += " "
	}
	for i, arg := range launch.Argv {
		if i > 0 {
			remote += " "
		}
		remote += ShellQuote(arg)
	}
	return remote
}

// StartProcess composes the leaf argv into one argv for the parent process
// executor. This makes arbitrary container/SSH nesting recursive and keeps the
// gRPC transport unaware of deployment kinds.
func (n *NestedExecutor) StartProcess(ctx context.Context, launch spec.ProcessLaunch) (spec.Process, error) {
	if err := validateProcessLaunch(launch); err != nil {
		return nil, err
	}
	if n.Parent == nil {
		return nil, errors.New("NestedExecutor: nil Parent")
	}
	parent, ok := n.Parent.(spec.ProcessExecutor)
	if !ok {
		return nil, fmt.Errorf("NestedExecutor parent %q: %w", n.Parent.Venue(), spec.ErrNotSupported)
	}
	var outer []string
	switch n.Jump.Kind {
	case JumpPodmanExec:
		outer = append([]string{"podman", "exec", "-i"}, n.Jump.ExtraArgs...)
		if launch.WorkingDir != "" {
			outer = append(outer, "--workdir", launch.WorkingDir)
		}
		for _, pair := range processEnvPairs(launch.Env) {
			outer = append(outer, "--env", pair)
		}
		outer = append(outer, n.Jump.Target)
	case JumpDockerExec:
		outer = append([]string{"docker", "exec", "-i"}, n.Jump.ExtraArgs...)
		if launch.WorkingDir != "" {
			outer = append(outer, "--workdir", launch.WorkingDir)
		}
		for _, pair := range processEnvPairs(launch.Env) {
			outer = append(outer, "--env", pair)
		}
		outer = append(outer, n.Jump.Target)
	case JumpSSH:
		outer = append([]string{"ssh", "-T"}, nestedSSHLogArgs()...)
		outer = append(outer, n.Jump.ExtraArgs...)
		outer = append(outer, n.Jump.Target)
		outer = append(outer, remoteLaunchCommand(launch))
	case JumpVirshConsole:
		return nil, fmt.Errorf("NestedExecutor process over virsh console: %w", spec.ErrNotSupported)
	default:
		return nil, fmt.Errorf("NestedExecutor process jump %d: %w", n.Jump.Kind, spec.ErrNotSupported)
	}
	if n.Jump.Kind == JumpPodmanExec || n.Jump.Kind == JumpDockerExec {
		outer = append(outer, launch.Argv...)
	}
	return parent.StartProcess(ctx, spec.ProcessLaunch{Argv: outer})
}

func processEnvPairs(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}
