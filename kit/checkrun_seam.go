package kit

import "github.com/opencharly/sdk/spec"

// checkrun_seam.go — the REPLY half of the "check-run" host↔plugin seam (P12). The
// REQUEST (spec.CheckRunRequest) is a CUE-sourced wire type; the reply is HAND-WRITTEN here
// with the check engine's result model, a wire-mandate exception the mandate's LEGAL path
// authorizes: a live `cue exp gengotypes` spike (throwaway #CheckResult/#StepResult defs, P12)
// PROVED CUE cannot faithfully express kit.CheckResult. The spike confirmed gengotypes CAN
// express the rest (the *spec.Op reference, time.Duration via @go override, the untagged
// PascalCase-no-omitempty Op/Verb/Status/Message/Elapsed via required fields) — but the
// engine-internal `DeadlineExceeded bool json:"-"` field has NO gengotypes equivalent
// (gengotypes emits `json:"deadline_exceeded,omitempty"`, never json:"-"). So an alias
// kit.CheckResult = spec.CheckResult would either LEAK the deadline-retry signal onto the wire
// (byte-compat break) or DROP the field the engine sets/reads in-memory (engine break). The
// reply therefore carries []StepResult VERBATIM — which also lets the plugin reuse the kit
// formatters (FormatStepResults*) with byte-parity across every --format and adds ZERO drift (a
// spec projection would duplicate the result model). command:check (candy/plugin-check) forwards
// a run to HostBuild("check-run"); the host builds the venue + runs the Runner and returns this
// reply, which the plugin formats + tallies into an exit code.

// CheckRunReply is the host-resolved result of a check-run. Steps is the per-step verdict
// list the plugin formats (FormatStepResults*) and tallies into an exit code. Image is the
// resolved image ref for the "Image: <ref>" header line. NoSteps signals the image declared no
// plan (the plugin prints "No plan steps defined for this image." and exits 0) — distinct from
// an empty Steps that ran zero scored steps. The host signals an infra error (bad image, engine
// failure) via the builder's error return, surfaced to the plugin.
type CheckRunReply struct {
	Steps   []StepResult `json:"steps,omitempty"`
	Image   string       `json:"image,omitempty"`
	NoSteps bool         `json:"no_steps,omitempty"`
	// Header is the pre-formatted, kind-specific banner line the host builds ("Image: X
	// (container: Y)" for pod, "VM: <name> (ssh …)", "Local deploy: …", "Group bed: …") from
	// data only the host holds (container name, ssh user/host/port, member count), so the
	// plugin stays kind-blind: it prints Header, then the formatted Steps.
	Header string `json:"header,omitempty"`
	// Passthrough carries the one non-plan-run live path — a nested pod-in-VM leaf whose check
	// the host delegates to the guest over SSH (`charly check live <pod>` run INSIDE the guest),
	// whose stdout/stderr + exit code the plugin forwards verbatim. Nil for every plan-run mode.
	Passthrough *StepPass `json:"passthrough,omitempty"`
	// Score is the "score"-mode reply (P12 Wave-2): the AI-harness SCORING result — RunCheckLive's
	// per-step verdicts (the substituted, nonce-carrying scoring plan walked host-side) the plugin
	// scorer consumes (summary, StepByID, Classify). Nil for the box/live/feature plan-run modes,
	// which carry their verdicts in Steps. CUE-sourced (spec.CheckRunResults) so ONE definition
	// serves both the host and the relocated plugin scorer (SDD; no alias).
	Score *spec.CheckRunResults `json:"score,omitempty"`
}

// StepPass is the verbatim stdout/stderr/exit-code of a host-delegated guest sub-invocation
// (the nested pod-in-VM check-live delegation, runVm's guestNestedCheckCmd path). The plugin
// writes Stdout/Stderr and returns ExitCode unchanged, so a guest-run check reports
// byte-identically to a direct one. Hand-written (not CUE): it is part of the kit reply model,
// which the wire mandate's spike keeps hand-written alongside CheckRunReply.
type StepPass struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}
