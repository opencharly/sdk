// Package kit is the importable contract a HOST-COUPLED plugin candy implements to
// run against charly's live check engine — the seam that lets a check verb whose
// logic needs the running deployment (exec-in-container, host TCP dial, host-vantage
// HTTP) live in its own candy module instead of charly's module.
//
// A host-coupled verb candy implements CheckVerbProvider; charly runs it in EITHER
// placement, invisibly above the registry: IN-PROCESS (compiled-in — charly passes
// the live *Runner as a CheckContext) OR OUT-OF-PROCESS (the CheckContext legs are
// served back to the candy over the host's reverse channel — ExecutorService for
// Exec + CheckContextService for HTTPDo/AddBackground, F2 — and the scalar legs ride
// the env_json snapshot). RunVerb is identical in both. This package imports only the
// stdlib + charly/spec (the generated param/Op types), so a candy module can import it
// without pulling charly's package main.
package kit

import (
	"encoding/json"
	"fmt"

	"github.com/opencharly/spec/spec"
)

// RunMode mirrors charly's RunMode: the mode a check runs under. The type (+ its consts
// + String()) is homed in the spec contract module (spec.CheckRunMode, checkcontext.go)
// with the rest of the check-verb contract cluster, so charly core references the contract
// while importing only spec; aliased here (with the const re-exports) so candy call sites
// compile UNCHANGED.
type RunMode = spec.CheckRunMode

const (
	// ModeLive — `charly check live`, against a running container/VM (in-container probes).
	ModeLive = spec.CheckModeLive
	// ModeBox — `charly check box`, against a disposable build container.
	ModeBox = spec.CheckModeBox
)

// Executor is the subset of charly's DeployExecutor a check verb needs (spec.CheckExecutor,
// checkcontext.go): run one command/script on the venue and capture stdout/stderr/exit
// separately. charly's DeployExecutor satisfies it structurally, so *Runner.Exec is passed
// straight through. Aliased here so candy call sites compile unchanged.
type Executor = spec.CheckExecutor

// GraphicsEndpoint is the resolved, dialable VM graphics endpoint a vnc/spice verb gets from
// CheckContext.ResolveGraphicsEndpoint — homed in the spec contract module
// (spec.CheckGraphicsEndpoint, checkcontext.go), aliased here for unchanged candy call sites.
type GraphicsEndpoint = spec.CheckGraphicsEndpoint

// CheckContext is the live check-engine surface a host-coupled verb's RunVerb consumes —
// homed in the spec contract module (spec.CheckContext, checkcontext.go) so charly core's
// reverse-channel dispatch references it while importing only spec; charly's *Runner
// implements it, and this alias keeps candy RunVerb signatures compiling unchanged.
type CheckContext = spec.CheckContext

// HTTPRequest is the host-vantage HTTP request a check verb hands cc.HTTPDo — homed in the
// spec contract module (spec.CheckHTTPRequest, checkcontext.go), aliased here unchanged.
type HTTPRequest = spec.CheckHTTPRequest

// HTTPResponse is the result of cc.HTTPDo — homed in the spec contract module
// (spec.CheckHTTPResponse, checkcontext.go), aliased here unchanged.
type HTTPResponse = spec.CheckHTTPResponse

// Status is a check verdict. It is the ONE pass/fail/skip enum for the check engine and
// every plugin candy — charly's CheckStatus is a type alias of it. FLOOR-SLIM Unit 4: Status
// itself (+ the iota consts + the String() method) moved to spec.Status
// (sdk/spec/status_result.go) as part of the CheckResult wire-envelope split — gengotypes has
// no construct for an iota enum + Stringer, so CUE owns the wire VALUE SET (a plain int) and
// Go owns the formatting behavior there. Kept as a type ALIAS here (not a repointed reference)
// so kit.Result + every out-of-process plugin candy's kit.Pass/Fail/Skip/StatusPass call sites
// compile UNCHANGED — this is a public SDK surface with ~15 external consumers, out of scope
// for an internal core-floor refactor.
type Status = spec.Status

const (
	StatusPass = spec.StatusPass
	StatusFail = spec.StatusFail
	StatusSkip = spec.StatusSkip
)

// Result is a host-coupled verb's verdict — homed in the spec contract module
// (spec.CheckVerbResult, checkcontext.go), aliased here so the Pass/Fail/Skip constructors
// and every candy call site compile unchanged. charly converts it to its internal
// CheckResult (stamping the Op/Verb/timing) at the dispatch boundary.
type Result = spec.CheckVerbResult

// Pass / Fail / Skip are the verdict constructors a verb returns; the *f variants
// take a printf format (mirror charly's passf/failf/skipf).
func Pass(msg string) Result { return Result{Status: StatusPass, Message: msg} }
func Fail(msg string) Result { return Result{Status: StatusFail, Message: msg} }
func Skip(msg string) Result { return Result{Status: StatusSkip, Message: msg} }

func Passf(format string, a ...any) Result { return Pass(fmt.Sprintf(format, a...)) }
func Failf(format string, a ...any) Result { return Fail(fmt.Sprintf(format, a...)) }
func Skipf(format string, a ...any) Result { return Skip(fmt.Sprintf(format, a...)) }

// ShellQuote moved to the fabric slice github.com/opencharly/spec/spec
// (shellquote.go, #55 step1) — a pure stdlib POSIX single-quoter is a fabric
// primitive single-sourced in the spec contract module; every former
// spec.ShellQuote caller now calls spec.ShellQuote.

// TrimPreview truncates s to a 200-char preview — homed in the fabric slice
// github.com/opencharly/spec/spec (the same fabric-primitive class as ShellQuote),
// re-exported here so kit.TrimPreview callers compile unchanged (R3, single source).
var TrimPreview = spec.TrimPreview

// WrapContainerCommand guards an in-container command-check script against stdin-consuming
// subcommands. RELOCATED to the spec contract module (spec.WrapContainerCommand, wrap_container.go,
// #55 CHECK-ENGINE cone Option A — a pure stdlib string guard charly core's check-op dispatch
// reaches importing zero kit); re-exported here so every existing kit.WrapContainerCommand call
// site (charly core + the candies + sdk) is untouched. New consumers should call
// spec.WrapContainerCommand directly.
var WrapContainerCommand = spec.WrapContainerCommand

// DecodeInput decodes an Op's plugin_input (map[string]any) into a candy's
// CUE-generated typed params struct via a JSON round-trip. A nil/empty input leaves
// out at its zero value; the host has already validated the input against the served schema.
func DecodeInput(in map[string]any, out any) {
	if len(in) == 0 {
		return
	}
	b, err := json.Marshal(in)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, out)
}

// CheckVerbProvider is the typed in-process contract a host-coupled check-verb candy
// implements — homed in the spec contract module (spec.CheckVerbProvider, checkcontext.go)
// so charly core's registry references it while importing only spec; aliased here so every
// candy implementing it (RunVerb signature) compiles unchanged.
type CheckVerbProvider = spec.CheckVerbProvider

// The TYPED-STEP state-provision contract cluster — StepKindName / StepKindServicePackaged /
// StepKindSystemPackages / ServicePackagedDesc / SystemPackagesDesc / ResolvePackageName /
// StepDescriptor / StepProvider / ProvisionActor — RELOCATED to the spec contract module
// (spec/spec/check_step.go, #55 CHECK-ENGINE cone Option A) and re-exported from
// sdk/kit/check_step_descriptors.go so charly core's in-proc kitVerbAdapter references them
// importing only spec while every candy call site compiles UNCHANGED. See that file.
