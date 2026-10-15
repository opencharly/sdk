package sdk

// exitcode.go — re-exports of the process exit-code contract relocated to
// github.com/opencharly/spec/exitcode (#55 import-purity). ExitCodeError and
// the check 0/1/2/3 convention live once in spec/exitcode; the sdk root
// re-exports them so candy call sites compile UNCHANGED.

import (
	"github.com/opencharly/spec/exitcode"
)

// ExitCodeError carries a specific PROCESS exit code from a command plugin's Invoke(OpRun) back to
// the host, which maps it to os.Exit(Code). A compiled-in command candy returns its error verbatim
// through the in-proc dispatch, but the host cannot classify the plugin's OWN error TYPES across the
// module boundary — so a command that must set a NON-1 exit code (the check 0/1/2/3 convention)
// wraps its failure in *ExitCodeError, which the host detects with errors.As and honors as the exit
// status. Code 0 falls back to the host's default error handling (no special code).
type ExitCodeError = exitcode.ExitCodeError

// The check-command exit-code convention (goss/pytest 0/1/2/3) is stated ONCE, in this module's
// contract source — github.com/opencharly/spec/exitcode. It is deliberately NOT restated here: the
// two copies had already DIVERGED, which is the argument. This one still said 3 meant only "a
// required host prerequisite is absent" after the check modes began reporting 3 for a plan
// resolution that came back EMPTY (an image that baked no `plan:`, a deployment whose source
// declares none, a bed child contributing no steps — opencharly/plugin-check#110), so a reader of
// the sdk copy learned a narrower contract than the code implements. A second statement of a
// contract is a second contract (R2); read the canonical table there.
//
//	CheckFailExitCode    = 2  the check RAN and one or more checks FAILED
//	CheckSkippedExitCode = 3  the phase RAN NOTHING — neither a pass nor a failure
const (
	CheckFailExitCode    = exitcode.CheckFailExitCode
	CheckSkippedExitCode = exitcode.CheckSkippedExitCode
)
