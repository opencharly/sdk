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
// subcommands. The runner delivers in-container scripts to the pod shell over a stdin heredoc
// ("stdin-attached exec"); without this guard the FIRST subcommand that reads stdin — adb shell,
// ssh, read, cat — consumes the REST of the heredoc (the not-yet-executed script lines), silently
// truncating the check to its first command. Wrapping the whole script in a brace group with stdin
// redirected from /dev/null fixes it generically: the shell reads the entire group before executing
// it (so the heredoc is fully drained by parse time), then runs every subcommand with stdin tied to
// /dev/null. The host path (a plain `sh -c` argv) is unaffected.
func WrapContainerCommand(script string) string {
	return "{ " + script + "\n} </dev/null"
}

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

// StepKindName names the TYPED install-plan step a step-providing verb lowers into. The
// host maps it to its internal StepKind enum; kept a string so the kit need not import
// charly's package main.
type StepKindName string

const (
	// StepKindServicePackaged — the `service` verb (enable a packaged unit; load-bearing reversals).
	StepKindServicePackaged StepKindName = "service-packaged"
	// StepKindSystemPackages — the `package` verb (install system packages).
	StepKindSystemPackages StepKindName = "system-packages"
)

// ServicePackagedDesc is the candy-decodable construction input for a service-packaged
// step: the host materializer adds the op-resolved scope + candy name and keeps the
// load-bearing Reverse() (disable / restore-enabled / remove-dropin) in package main.
type ServicePackagedDesc struct {
	Unit   string
	Enable bool
}

// SystemPackagesDesc is the candy-decodable construction input for a system-packages step
// (the `package` verb): the authored package name + per-distro map. The host materializer
// resolves the cross-distro name (ResolvePackageName against the image's tags), sets the
// image format + PhaseInstall, and builds the SystemPackagesStep.
type SystemPackagesDesc struct {
	Package    string
	PackageMap map[string]string
}

// ResolvePackageName picks the correct package name for the running image's distro: if
// packageMap has a key matching any of the image's distro tags (first match wins — tags
// are authored most-specific-first, "fedora:43" before "fedora"), that mapping is used;
// otherwise the bare pkg name. The single cross-distro name resolver shared by the
// `package` candy's check + act AND the host's step materializer (R3).
func ResolvePackageName(pkg string, packageMap map[string]string, distros []string) string {
	if len(packageMap) == 0 {
		return pkg
	}
	for _, tag := range distros {
		if name, ok := packageMap[tag]; ok && name != "" {
			return name
		}
	}
	return pkg
}

// StepDescriptor is the candy-decodable construction input for a TYPED install-plan step
// (the build/deploy install timeline). Exactly one variant is non-nil; the host
// materializer rebuilds the real package-main InstallStep from it (computing the
// package-main-only inputs — scope from op.RunAs+img, candy name — and keeping the
// load-bearing Reverse() in package main, so the candy never imports an IR type).
type StepDescriptor struct {
	ServicePackaged *ServicePackagedDesc
	SystemPackages  *SystemPackagesDesc
}

// StepProvider is the OPTIONAL third role of a host-coupled verb candy: a verb whose
// build/deploy ACT lowers into a TYPED install-plan step (service → service-packaged,
// package → system-packages) rather than a shell (ProvisionActor) or a generic OpStep.
// StepKind names the target step (static); ConstructStepDescriptor returns the
// candy-decodable construction inputs for one op. The host wraps a candy implementing
// this in an adapter that satisfies package-main's TypedStepProvider, materializing the
// descriptor into the real IR step.
type StepProvider interface {
	StepKind() StepKindName
	ConstructStepDescriptor(op *spec.Op) StepDescriptor
}

// ProvisionActor is the OPTIONAL second role of a host-coupled verb candy: the do:act
// renderer for a state-provision verb (kernel_param/mount/user/unix_group/file/command/
// service/package), rendering the shell that ENACTS the op under the live init / package
// manager. It is reached at install COMPILE+EMIT (a `run: {plugin: <verb>}` step → the
// build-act RUN in emitTasks, and the local/vm deploy act) AND at runtime act. A candy
// whose verb type implements this ALONGSIDE CheckVerbProvider is registered as a
// multi-role provider (the host adapter then also satisfies the package-main
// ProvisionActor). op is the spec.Op (the verb's plugin_input rides op.PluginInput);
// distros is the image's distro tag list for package-name resolution. Returns
// (script, ok); ok=false means "no act form for this op" (the host skips/errors per its
// act path). This is the SHELL-string act role — a verb that instead lowers into a typed
// InstallPlan step (service/package) additionally needs the kit step contract.
type ProvisionActor interface {
	Reserved() string
	RenderProvisionScript(op *spec.Op, distros []string) (script string, ok bool)
}
