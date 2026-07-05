package sdk

import (
	"fmt"
	"strings"

	pb "github.com/opencharly/sdk/proto"
	"github.com/opencharly/sdk/spec"
)

// Preview truncates a string to 400 chars (adding an ellipsis) for verdict error messages —
// the shared truncation each live-verb plugin formerly copied.
func Preview(s string) string {
	s = strings.TrimSpace(s)
	const max = 400
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// VerbVerdict is the shared exit/stdout/stderr/artifact verdict pipeline every live check verb
// runs after dispatching an op — the byte-identical block cdp/vnc/wl/dbus/record/… each carried,
// hoisted here (R3). It maps runErr → exit 1 + stderr, compares exit against the authored
// op.ExitStatus, runs the op.Stdout/op.Stderr MatchAll validators and (when artifact is true) the
// artifact validators, and returns the {status,message} reply — "fail" naming the first mismatch,
// "pass" with a non-empty body (out, else stderr, else a synthetic "<verb> <method>: exit=N").
// `verb` prefixes every message ("cdp: screenshot: exit=…"); the caller passes whether THIS method
// produces an artifact (e.g. method == "screenshot").
func VerbVerdict(verb, method, out string, runErr error, op *spec.Op, artifact bool) (*pb.InvokeReply, error) {
	exit := 0
	stderr := ""
	if runErr != nil {
		exit = 1
		stderr = runErr.Error()
	}
	wantExit := 0
	if op.ExitStatus != nil {
		wantExit = *op.ExitStatus
	}
	if exit != wantExit {
		return ResultJSON("fail", fmt.Sprintf("%s: %s: exit=%d, want %d (stderr: %s)", verb, method, exit, wantExit, Preview(stderr)))
	}
	if err := MatchAll(out, op.Stdout); err != nil {
		return ResultJSON("fail", fmt.Sprintf("%s: %s: stdout: %v (got: %s)", verb, method, err, Preview(out)))
	}
	if err := MatchAll(stderr, op.Stderr); err != nil {
		return ResultJSON("fail", fmt.Sprintf("%s: %s: stderr: %v (got: %s)", verb, method, err, Preview(stderr)))
	}
	if artifact {
		if err := RunArtifactValidators(op); err != nil {
			return ResultJSON("fail", fmt.Sprintf("%s: %s: %v", verb, method, err))
		}
	}
	body := out
	if strings.TrimSpace(body) == "" {
		body = stderr
	}
	if strings.TrimSpace(body) == "" {
		body = fmt.Sprintf("%s %s: exit=%d", verb, method, exit)
	}
	return ResultJSON("pass", body)
}
