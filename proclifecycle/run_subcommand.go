package proclifecycle

import (
	"os"
	"os/exec"
)

// RunCharlySubcommand shells out to the CURRENT binary (os.Args[0]) with args,
// inheriting stdin/stdout/stderr. Relocated from charly core (run_subcommand.go)
// as a stdlib-only host-exec leaf: the host-side update/deploy orchestration
// (podUpdateCmd, the unified-target Update/Rebuild methods, per-kind R10
// sequences, `charly vm cycle`) spawns child `charly <args…>` invocations
// through it, and using os.Args[0] means an update loop picks up the local
// build-under-test automatically.
//
// A package var (not a plain func) so tests can stub the child-process boundary
// (e.g. record the image-build / vm-cp-box calls a deploy makes without actually
// spawning the binary).
var RunCharlySubcommand = func(args ...string) error {
	exe := os.Args[0]
	cmd := exec.Command(exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
