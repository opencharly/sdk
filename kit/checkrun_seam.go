package kit

// checkrun_seam.go — re-export of the check-run REPLY wire types, RELOCATED to the spec
// contract module (#55 CHECK-ENGINE cone Option A). Both the request (spec.CheckRunRequest) AND
// the reply envelope (CheckRunReply / StepResult / StepPass) are CUE-sourced in
// spec/schema/checkresult.cue (#CheckRunReply / #StepResult / #StepPass), generated into
// spec/spec/cue_types_gen.go — a live `cue exp gengotypes` spike proves the generated structs are
// byte-identical to the former hand-written wire types (the `optional=nillable` marker emits the
// Passthrough / Score pointers). The SDD wire-mandate exception is narrowed to EXACTLY
// kit.CheckResult's engine-internal `DeadlineExceeded bool json:"-"` (gengotypes cannot emit
// json:"-"), the one field with no gengotypes equivalent: the reply wire form carries the
// spec.CheckResult fields only and the DeadlineExceeded flag lives on the kit-internal engine
// CheckResult, dropped at the StepResult boundary exactly as it was on the wire before —
// byte-identical output, R3.
// charly core's former check-run seam referenced spec.CheckRunReply importing only spec; kit
// re-exports each here so every existing kit.CheckRunReply / kit.StepPass call
// site (the candies) is untouched. The reply carries []StepResult VERBATIM so the
// plugin reuses the kit formatters (FormatStepResults*) with byte-parity across every --format.
// command:check (candy/plugin-check) runs every mode plugin-side (K-wave 2 cone R4 — the
// "check-run" HostBuild kind is deleted) and returns this reply, which the plugin formats +
// tallies into an exit code.

import "github.com/opencharly/spec/spec"

// CheckRunReply is the host-resolved result of a check-run. Aliased to spec.CheckRunReply (the
// body lives there).
type CheckRunReply = spec.CheckRunReply

// StepPass is the verbatim stdout/stderr/exit-code of a host-delegated guest sub-invocation.
// Aliased to spec.StepPass (the body lives there).
type StepPass = spec.StepPass
