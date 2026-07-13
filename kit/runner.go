package kit

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/opencharly/sdk/spec"
)

// runner.go — the check-engine driver, relocated from charly core (P12). Runner is the
// importable library form of the plan walk's host context: it implements PlanContext (the
// surface RunOne/RunPlan consume) and carries the shared engine state, so any plugin candy that
// runs a plan drives the SAME loop the check engine uses. The host-coupled surfaces stay in
// charly core behind THREE injected seams — Verbs (the provider-registry verb dispatch), Grammar
// (the VerbCatalog do-mode/context grammar), and TargetResolver (the check_venue host atom for
// the `on:` per-step venue swap).
//
// The live-check VERB surface (kit.CheckContext) and its 5 host-reverse legs (HTTPDo + the four
// Resolve* podman/go-libvirt/ssh ops) are NOT on Runner: they need machinery a plugin module
// lacks, so charly core keeps a thin host wrapper that WRAPS *Runner (charly's hostCheckContext)
// and reads Runner's engine state through the exported accessors below. That host wrapper and the
// out-of-process checkContextReverseServer both consume the SAME core resolveVerb* functions —
// one source, two consumers, endpoint-identical (R3).
//
// Runner is CONSTRUCTOR-SHAPED: its state is unexported and its interface methods (Mode/Distros/
// VerifyOnly/…) would collide with same-named data fields (the exact reason the former
// runnerPlanContext/runnerCheckContext were wrappers), so the host builds it via NewRunner(cfg)
// and reaches its state through methods — the genuinely-library-shaped API a plugin gets too.

// runnerProbeNeverHangFallback is the defensive per-probe-attempt ceiling used only when the
// host leaves cfg.ProbeTimeout zero. The host ALWAYS sets it from its readiness config
// (loadedReadiness().PerAttemptFor), so this fallback is never hit on the real paths; it exists
// so a bare NewRunner (a test / a plugin) still bounds a wedged probe. Kept in kit (not a
// core-const reference) to keep kit standalone (R3 — the host owns its own readiness table).
const runnerProbeNeverHangFallback = 90 * time.Second

// PlanGrammar is the do-mode + execution-context grammar seam. charly core's VerbCatalog impl
// stays core (ExecContext lives in deploykit, which imports kit, so the concrete context enum
// never crosses this seam: the context predicate is a bool and the skip-message contexts a
// pre-formatted string).
type PlanGrammar interface {
	// EffectiveDo resolves op's do-mode (the keyword-stamped intentDo wins, else the verb's
	// VerbCatalog default, else DoAssert).
	EffectiveDo(op *spec.Op) DoMode
	// InContext reports whether op is legal in the run's active context: runtime=true → the
	// live (runtime) context, runtime=false → the box (build) context.
	InContext(op *spec.Op, runtime bool) bool
	// ContextsLabel is op's effective-contexts list pre-formatted for the context-skip message.
	ContextsLabel(op *spec.Op) string
}

// VenueResolver retargets a per-step venue (the `on:` modifier / venue swap) to a host executor,
// its resolved variable env, and whether that env carries runtime state. venue→executor is the
// host check_venue atom and the env is host var resolution — both stay core; Runner drives the
// swap through this seam. An unknown venue returns a non-nil err.
type VenueResolver func(venue string) (exec Executor, env map[string]string, hasRuntime bool, err error)

// RunnerConfig carries every field a Runner is built with. The host fills it (folding the former
// post-construction field-pokes + attachCheckRunnerContext); zero DialTimeout/ProbeTimeout/
// HTTPClient fall back to sensible defaults in NewRunner.
type RunnerConfig struct {
	Exec Executor
	Mode RunMode

	// Env is the resolved variable map (the host CheckVarResolver.Env); HasRuntime mirrors
	// CheckVarResolver.HasRuntime (false → runtime-only vars resolve to an unresolved skip).
	Env        map[string]string
	HasRuntime bool

	Distros  []string
	Box      string
	Instance string
	// VmName is the VM domain-target name the host vm/spice/libvirt verb legs address for a VM
	// deployment: the caller sets it to the already-resolved per-deploy domain identity
	// (charly-<VmName> is the live libvirt domain). Empty for non-VM deployments, where
	// VmTargetName falls back to Box.
	VmName string

	HostVars map[string]string
	// CandyDirs maps candy name → resolved source dir (relative committed-APK anchoring);
	// CandyScanErr is its build error (only an apk-anchoring check consults it). Host-read.
	CandyDirs    map[string]string
	CandyScanErr error

	VerifyOnly           bool
	SkipDeterministicRun bool

	Scenario *ScenarioContext
	Grader   StepGrader

	// Injected host seams (all required for a live run; a bare-Op test may leave Grammar/
	// TargetResolver nil — SwapVenue/ContextSkipReason then no-op, matching the classical path).
	Verbs          VerbResolver
	Grammar        PlanGrammar
	TargetResolver VenueResolver

	// Optional overrides — zero values take the NewRunner defaults.
	DialTimeout  time.Duration
	ProbeTimeout time.Duration
	HTTPClient   *http.Client
}

// Runner implements PlanContext — locked at compile time (the host also passes it, wrapped in a
// thin CheckContext, to verb dispatch).
var _ PlanContext = (*Runner)(nil)

// Runner wires the execution context for one pass of checks. Constructor-shaped: build it with
// NewRunner and reach its state through the methods below.
type Runner struct {
	exec         Executor
	mode         RunMode
	env          map[string]string
	hasRuntime   bool
	distros      []string
	box          string
	instance     string
	vmName       string
	hostVars     map[string]string
	candyDirs    map[string]string
	candyScanErr error

	verifyOnly           bool
	skipDeterministicRun bool

	scenario *ScenarioContext
	grader   StepGrader

	verbs          VerbResolver
	grammar        PlanGrammar
	targetResolver VenueResolver

	httpClient   *http.Client
	dialTimeout  time.Duration
	probeTimeout time.Duration
}

// NewRunner constructs a Runner from cfg, applying defaults for a zero HTTPClient (10s),
// DialTimeout (3s), and ProbeTimeout (the fallback ceiling; the host normally supplies its
// readiness-derived value).
func NewRunner(cfg RunnerConfig) *Runner {
	r := &Runner{
		exec:                 cfg.Exec,
		mode:                 cfg.Mode,
		env:                  cfg.Env,
		hasRuntime:           cfg.HasRuntime,
		distros:              cfg.Distros,
		box:                  cfg.Box,
		instance:             cfg.Instance,
		vmName:               cfg.VmName,
		hostVars:             cfg.HostVars,
		candyDirs:            cfg.CandyDirs,
		candyScanErr:         cfg.CandyScanErr,
		verifyOnly:           cfg.VerifyOnly,
		skipDeterministicRun: cfg.SkipDeterministicRun,
		scenario:             cfg.Scenario,
		grader:               cfg.Grader,
		verbs:                cfg.Verbs,
		grammar:              cfg.Grammar,
		targetResolver:       cfg.TargetResolver,
		httpClient:           cfg.HTTPClient,
		dialTimeout:          cfg.DialTimeout,
		probeTimeout:         cfg.ProbeTimeout,
	}
	if r.httpClient == nil {
		r.httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if r.dialTimeout <= 0 {
		r.dialTimeout = 3 * time.Second
	}
	if r.probeTimeout <= 0 {
		r.probeTimeout = runnerProbeNeverHangFallback
	}
	return r
}

// --- PlanContext implementation (the walk's driver surface) -----------------

// Distros is the image's distro tag list, for the exclude_distros: skip.
func (r *Runner) Distros() []string { return r.distros }

// Mode selects ModeLive vs ModeBox routing.
func (r *Runner) Mode() RunMode { return r.mode }

// VerifyOnly restricts the walk to idempotent verify steps (check:/agent-check:).
func (r *Runner) VerifyOnly() bool { return r.verifyOnly }

// SkipDeterministicRun skips deterministic run: install-timeline steps (feature-run mode).
func (r *Runner) SkipDeterministicRun() bool { return r.skipDeterministicRun }

// Scenario is the per-run capture/var context; SetScenario installs one for a RunPlan walk.
func (r *Runner) Scenario() *ScenarioContext      { return r.scenario }
func (r *Runner) SetScenario(sc *ScenarioContext) { r.scenario = sc }

// Verbs is the verb-dispatch seam; Grader is the agent-step grader (nil when unbound).
func (r *Runner) Verbs() VerbResolver { return r.verbs }
func (r *Runner) Grader() StepGrader  { return r.grader }

// EffectiveDo resolves op's do-mode via the injected grammar (DoAssert when no grammar is wired).
func (r *Runner) EffectiveDo(op *spec.Op) DoMode {
	if r.grammar == nil {
		return DoAssert
	}
	return r.grammar.EffectiveDo(op)
}

// ContextSkipReason returns a non-empty skip message when op's effective execution context is not
// active in the run's mode (box→build, live→runtime); "" means the op runs. The mode-composition
// and the message formatting are kit-side; only the VerbCatalog context predicate crosses the
// grammar seam (a nil grammar — a bare-Op test — never skips on context).
func (r *Runner) ContextSkipReason(op *spec.Op) string {
	if r.grammar == nil {
		return ""
	}
	runtime := r.mode != ModeBox
	modeName := "live"
	if r.mode == ModeBox {
		modeName = "box"
	}
	if !r.grammar.InContext(op, runtime) {
		return fmt.Sprintf("context %s not active in %s mode", r.grammar.ContextsLabel(op), modeName)
	}
	return ""
}

// EffectiveEnv builds the variable-expansion env for the current step: the resolver base overlaid
// with cross-deployment ${HOST:…} addresses (per-run, target-independent) then the scenario
// captures (which win on a key collision). Copy-on-overlay keeps the base map clean across runs.
func (r *Runner) EffectiveEnv() map[string]string {
	base := r.env
	if r.scenario == nil && len(r.hostVars) == 0 {
		return base
	}
	env := make(map[string]string, len(base)+len(r.hostVars)+2)
	maps.Copy(env, base)
	maps.Copy(env, r.hostVars)
	if r.scenario != nil {
		r.scenario.ApplyToEnv(env)
	}
	return env
}

// ProbeNeverHang is the per-probe-attempt never-hang ceiling for op. It is NOT the probe's
// semantic timeout (the http client, dial timeout, a verb's own timeout:, and the eventually:
// retry all operate INSIDE it) — it is the kill-switch for a probe that wedges in its data phase.
// A longer author-declared timeout: is honored over the floor so a slow probe is never cut short.
func (r *Runner) ProbeNeverHang(op *spec.Op) time.Duration {
	floor := r.probeTimeout
	if floor <= 0 {
		floor = runnerProbeNeverHangFallback
	}
	if op != nil && op.Timeout != "" {
		if d, err := time.ParseDuration(op.Timeout); err == nil && d+30*time.Second > floor {
			return d + 30*time.Second
		}
	}
	return floor
}

// SwapVenue retargets the executor + env + image to op's per-step venue for the duration of one
// dispatch, returning a restore func (nil when no swap) and a non-empty failReason when the venue
// cannot be resolved. It mutates the Runner in place so EffectiveEnv + the verb dispatch (which
// read exec/env/box) see the swapped venue — the same self-swap guard the classical inline path
// used (venue set, differs from the active target, and a TargetResolver is wired).
func (r *Runner) SwapVenue(op *spec.Op) (func(), string) {
	if op.Venue == "" || op.Venue == r.box || r.targetResolver == nil {
		return nil, ""
	}
	newExec, newEnv, newHasRuntime, terr := r.targetResolver(op.Venue)
	if terr != nil {
		return nil, fmt.Sprintf("venue %q — %v", op.Venue, terr)
	}
	origExec, origEnv, origHasRuntime, origBox := r.exec, r.env, r.hasRuntime, r.box
	if newExec != nil {
		r.exec = newExec
	}
	if newEnv != nil {
		r.env = newEnv
		r.hasRuntime = newHasRuntime
	}
	r.box = op.Venue
	return func() {
		r.exec = origExec
		r.env = origEnv
		r.hasRuntime = origHasRuntime
		r.box = origBox
	}, ""
}

// --- engine-state accessors (read by the host CheckContext wrapper + verb dispatch) ----------

// Exec is the venue executor for the current (possibly venue-swapped) target.
func (r *Runner) Exec() Executor { return r.exec }

// Box / Instance are the deployment's image + instance names (empty under ModeBox).
func (r *Runner) Box() string      { return r.box }
func (r *Runner) Instance() string { return r.instance }

// VmName is the caller-set VM domain-target (the resolved per-deploy domain identity; empty for
// non-VM deployments); VmTargetName falls back to Box, the name the host vm/spice legs hand the
// plugin as the libvirt-domain target.
func (r *Runner) VmName() string { return r.vmName }
func (r *Runner) VmTargetName() string {
	if r.vmName != "" {
		return r.vmName
	}
	return r.box
}

// DialTimeout is the per-dial ceiling for host-side TCP reachability probes. HTTPClient is the
// engine's base client (the host HTTPDo leg derives per-request clients from it). HasRuntime
// reports whether the resolved env carries running-container state (false → runtime-only vars
// resolve to a skip). CandyDirs / CandyScanErr anchor a relative committed-APK path.
func (r *Runner) DialTimeout() time.Duration   { return r.dialTimeout }
func (r *Runner) HTTPClient() *http.Client     { return r.httpClient }
func (r *Runner) HasRuntime() bool             { return r.hasRuntime }
func (r *Runner) CandyDirs() map[string]string { return r.candyDirs }
func (r *Runner) CandyScanErr() error          { return r.candyScanErr }

// --- run entries ------------------------------------------------------------

// Run executes the supplied checks sequentially and returns per-check results. It does not
// short-circuit on failure — the report shows every check's outcome. The per-check walk lives in
// RunOne; Runner is its driver (it implements PlanContext).
func (r *Runner) Run(ctx context.Context, checks []spec.Op) []CheckResult {
	results := make([]CheckResult, 0, len(checks))
	for i := range checks {
		results = append(results, RunOne(ctx, r, &checks[i]))
	}
	return results
}
