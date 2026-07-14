package sdk

import (
	"errors"
	"io"
	"testing"

	"github.com/alecthomas/kong"
)

// clidispatch_test.go — the #52 regression gate. Every case here FAILS against the old
// per-plugin `kong.Exit(func(int){})` no-op (a `--help` would fall through to the leaf's Run or
// to a spurious parse error). The panic-sentinel in clidispatch.go makes them pass.

// dispatchRan records whether a leaf's Run() actually executed. Reset per case (tests are serial).
var dispatchRan bool

type okLeaf struct{}

func (okLeaf) Run() error { dispatchRan = true; return nil }

type failLeaf struct{}

func (failLeaf) Run() error {
	dispatchRan = true
	return &ExitCodeError{Code: 7, Err: errors.New("boom")}
}

type dispatchTestCLI struct {
	Ok   okLeaf   `cmd:"" help:"ok leaf"`
	Fail failLeaf `cmd:"" help:"fail leaf"`
}

// quiet discards kong's help/usage output so the test log stays clean.
func quiet() kong.Option { return kong.Writers(io.Discard, io.Discard) }

func TestRunInProcCLI_LeafHelpExitsZeroWithoutRunning(t *testing.T) {
	dispatchRan = false
	var cli dispatchTestCLI
	if err := RunInProcCLI("t", &cli, []string{"ok", "--help"}, quiet()); err != nil {
		t.Fatalf("leaf --help: want nil, got %v", err)
	}
	if dispatchRan {
		t.Fatal("leaf --help ran the command's Run() — the exact #52 bug")
	}
}

func TestRunInProcCLI_TopLevelHelpExitsZeroWithoutRunning(t *testing.T) {
	dispatchRan = false
	var cli dispatchTestCLI
	if err := RunInProcCLI("t", &cli, []string{"--help"}, quiet()); err != nil {
		t.Fatalf("top-level --help: want nil, got %v", err)
	}
	if dispatchRan {
		t.Fatal("top-level --help ran a command's Run()")
	}
}

func TestRunInProcCLI_RealInvocationRuns(t *testing.T) {
	dispatchRan = false
	var cli dispatchTestCLI
	if err := RunInProcCLI("t", &cli, []string{"ok"}, quiet()); err != nil {
		t.Fatalf("real invocation: want nil, got %v", err)
	}
	if !dispatchRan {
		t.Fatal("real invocation did NOT run the leaf")
	}
}

func TestRunInProcCLI_LeafErrorPropagatesExitCode(t *testing.T) {
	dispatchRan = false
	var cli dispatchTestCLI
	err := RunInProcCLI("t", &cli, []string{"fail"}, quiet())
	if !dispatchRan {
		t.Fatal("fail leaf did not run")
	}
	var ece *ExitCodeError
	if !errors.As(err, &ece) || ece.Code != 7 {
		t.Fatalf("want *ExitCodeError{Code:7}, got %v", err)
	}
}

func TestRunInProcCLI_ParseErrorPropagates(t *testing.T) {
	dispatchRan = false
	var cli dispatchTestCLI
	if err := RunInProcCLI("t", &cli, []string{"nope"}, quiet()); err == nil {
		t.Fatal("unknown subcommand: want a parse error, got nil")
	}
	if dispatchRan {
		t.Fatal("a parse error still ran a leaf")
	}
}

func TestParseInProcCLI_HelpIsDoneNoError(t *testing.T) {
	var cli dispatchTestCLI
	done, err := ParseInProcCLI("t", &cli, []string{"ok", "--help"}, quiet())
	if err != nil {
		t.Fatalf("--help: want nil err, got %v", err)
	}
	if !done {
		t.Fatal("--help: want done=true (caller must stop), got false")
	}
}

func TestParseInProcCLI_RealParseNotDone(t *testing.T) {
	var cli dispatchTestCLI
	done, err := ParseInProcCLI("t", &cli, []string{"ok"}, quiet())
	if err != nil {
		t.Fatalf("real parse: want nil err, got %v", err)
	}
	if done {
		t.Fatal("real parse: want done=false, got true")
	}
}

func TestParseInProcCLI_ParseErrorPropagates(t *testing.T) {
	var cli dispatchTestCLI
	done, err := ParseInProcCLI("t", &cli, []string{"nope"}, quiet())
	if err == nil {
		t.Fatal("unknown subcommand: want a parse error, got nil")
	}
	if done {
		t.Fatal("a parse error must not report done=true")
	}
}
