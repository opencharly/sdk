package kit

// checkrun_seam.go — re-export of the check-run REPLY wire types, RELOCATED to the spec
// contract module github.com/opencharly/spec/spec/checkresult_wire.go (#55 CHECK-ENGINE cone
// Option A). The REQUEST (spec.CheckRunRequest) is a CUE-sourced wire type; the reply
// (CheckRunReply/StepPass) is HAND-WRITTEN (the wire-mandate exception the mandate's LEGAL path
// authorizes — see spec/spec/checkresult_wire.go's header for the gengotypes-can't-emit-json:"-"
// rationale, carried here unchanged): kit.CheckResult's engine-internal `DeadlineExceeded bool
// json:"-"` has no gengotypes equivalent, so the wire form carries the spec.CheckResult fields
// only and the DeadlineExceeded flag lives on the kit-internal engine CheckResult, dropped at
// the StepResult boundary exactly as it was on the wire before — byte-identical output, R3.
// charly core's check-run seam (host_build_check_run.go) references spec.CheckRunReply importing
// only spec; kit re-exports each here so every existing kit.CheckRunReply / kit.StepPass call
// site (charly core + the candies) is untouched. The reply carries []StepResult VERBATIM so the
// plugin reuses the kit formatters (FormatStepResults*) with byte-parity across every --format.
// command:check (candy/plugin-check) forwards a run to HostBuild("check-run"); the host builds
// the venue + runs the Runner and returns this reply, which the plugin formats + tallies into an
// exit code.

import "github.com/opencharly/spec/spec"

// CheckRunReply is the host-resolved result of a check-run. Aliased to spec.CheckRunReply (the
// body lives there).
type CheckRunReply = spec.CheckRunReply

// StepPass is the verbatim stdout/stderr/exit-code of a host-delegated guest sub-invocation.
// Aliased to spec.StepPass (the body lives there).
type StepPass = spec.StepPass