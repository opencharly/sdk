package kit

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/opencharly/sdk/spec"
)

// planrun.go — the check-engine plan walk, dissolved out of charly core into sdk/kit so ANY
// plugin candy that runs a plan drives the SAME loop the check engine uses. The walk
// (RunOne / RunPlan / runUnit) reaches the host through three interfaces: PlanContext (the
// driver surface — mode, distros, env, venue swap, per-verb do-mode), VerbResolver (the
// verb-dispatch seam, satisfied by the core provider registry — the plan's ONE named
// blocker), and StepGrader (the agent-step grader). The host-serving atoms
// (endpoint/venue/container resolution, HTTP-do, the provider registry, the CUE loader) STAY
// in core behind those seams; only the vocabulary-independent walk lives here.

// DoMode is the act/assert/instruct axis. act = perform a side-effect; assert = run the
// matchers (read-only); instruct = hand free-form text to the agent grader.
type DoMode string

const (
	DoAct      DoMode = "act"
	DoAssert   DoMode = "assert"
	DoInstruct DoMode = "instruct"
)

// GraderRequest is the agent grader's input for one agent-run:/agent-check: step.
type GraderRequest struct {
	Description string
	Keyword     string
	Text        string
	// ReadOnly is true for agent-check: (assessment only), false for agent-run: (may mutate).
	ReadOnly bool
}

// StepGrader judges an agent step (agent-run:/agent-check:). The host impl spawns the
// configured kind:agent CLI to probe the live target and return a pass/fail verdict; nil
// when no grader is bound (agent steps then advisory-skip, or fail under strict).
type StepGrader interface {
	Grade(ctx context.Context, req GraderRequest) CheckResult
}

// VerbResolver is the verb-dispatch seam — the ONE thing the walk needs from the core
// provider registry. The core impl resolves the op's verb word and runs it (in-proc
// CheckVerbProvider fast path or out-of-process Invoke envelope), threading the live host
// CheckContext it owns; the walk never sees a provider type or the registry.
type VerbResolver interface {
	// RunVerb resolves op's verb word and runs it, returning (result, true). (_, false)
	// means no such verb is registered — the walk reports the op as an unknown-verb skip.
	RunVerb(ctx context.Context, op *spec.Op) (CheckResult, bool)
	// RunProvisionAct runs a do:act state-provision verb's act (create/configure) and
	// returns (result, true). (_, false) means the verb has no act path — the walk falls
	// through to the assert dispatch (the handler IS the act for action verbs).
	RunProvisionAct(ctx context.Context, op *spec.Op, verb string) (CheckResult, bool)
}

// PlanContext is the host-driver surface the plan walk consumes. The core *Runner implements
// it; a plugin running a plan in-proc supplies its own impl. Every method is kind-blind — no
// concrete kind, no provider word, no ExecContext crosses this interface (ExecContext lives
// in deploykit, which imports kit; the grammar that consults it stays core behind
// ContextSkipReason / EffectiveDo).
type PlanContext interface {
	// Distros is the image's distro tag list, for the exclude_distros: skip.
	Distros() []string
	// Mode selects RunModeLive vs RunModeBox routing.
	Mode() RunMode
	// VerifyOnly restricts a plan walk to idempotent verify steps (check:/agent-check:),
	// skipping mutating steps (run:/agent-run:) — the check live / check box mode.
	VerifyOnly() bool
	// SkipDeterministicRun skips deterministic run: install-timeline steps while still
	// running check:/agent-check: and the agent-graded agent-run: — the feature-run mode.
	SkipDeterministicRun() bool
	// ContextSkipReason returns a non-empty skip message when the op's effective execution
	// context is not active in the current mode (box→build, live→runtime); "" means the op
	// runs. Wraps the core VerbCatalog grammar so ExecContext never crosses this seam.
	ContextSkipReason(op *spec.Op) string
	// EffectiveDo resolves the op's do-mode (the keyword-stamped intentDo, else the verb's
	// VerbCatalog default, else DoAssert). Wraps the core grammar.
	EffectiveDo(op *spec.Op) DoMode
	// EffectiveEnv builds the variable-expansion env for the current step (the resolver base
	// overlaid with cross-deployment ${HOST:…} addresses + the scenario captures).
	EffectiveEnv() map[string]string
	// ProbeNeverHang is the per-probe-attempt never-hang ceiling for op.
	ProbeNeverHang(op *spec.Op) time.Duration
	// SwapVenue retargets the host executor/resolver to op's per-step venue for the duration
	// of one dispatch, returning a restore func (nil when no swap) and a non-empty failReason
	// when the venue could not be resolved (the op is reported FAIL). The core impl mutates
	// the *Runner it wraps, so EffectiveEnv + the verb dispatch see the swapped venue.
	SwapVenue(op *spec.Op) (restore func(), failReason string)
	// Scenario is the per-run capture/var context; SetScenario installs a fresh one for a
	// RunPlan walk (restored by the caller).
	Scenario() *ScenarioContext
	SetScenario(sc *ScenarioContext)
	// Verbs is the verb-dispatch seam; Grader is the agent-step grader (nil when unbound).
	Verbs() VerbResolver
	Grader() StepGrader
}

// LabeledDescription is one entity's baked plan with its collection-time origin + description.
type LabeledDescription struct {
	Origin      string      `json:"origin"`
	Description string      `json:"description,omitempty"`
	Plan        []spec.Step `json:"plan,omitempty"`
}

// LabelDescriptionSet groups the collected + include-expanded + overlay-merged plans by the
// entity layer they came from (candy → box → deploy), the walk order RunPlan flattens.
type LabelDescriptionSet struct {
	Candy  []LabeledDescription `json:"candy,omitempty"`
	Box    []LabeledDescription `json:"box,omitempty"`
	Deploy []LabeledDescription `json:"deploy,omitempty"`
}

// IsEmpty reports whether the set carries no labeled descriptions in any layer.
func (s *LabelDescriptionSet) IsEmpty() bool {
	if s == nil {
		return true
	}
	return len(s.Candy) == 0 && len(s.Box) == 0 && len(s.Deploy) == 0
}

// flatStep carries a plan step with its collection-time origin + the owning entity's
// description (for the agent grader).
type flatStep struct {
	origin string
	desc   string
	idx    int
	step   spec.Step
}

// RunOne handles all the per-check housekeeping (verb resolution, skip handling, variable
// expansion, venue swap, do-mode routing) and dispatches to a verb handler through the
// VerbResolver seam. The `eventually:` retry wraps the verb dispatch when requested.
//
//nolint:gocyclo // verb dispatch router with venue-swap and eventually: retry wrapper; branching is essential to the execution model
func RunOne(ctx context.Context, pc PlanContext, c *spec.Op) CheckResult {
	start := time.Now()
	kind, err := c.Kind()
	result := CheckResult{Op: c, Verb: kind}
	settle := func() CheckResult {
		result.Elapsed = time.Since(start)
		result.Attempts = 1
		result.TotalElapsed = result.Elapsed
		return result
	}
	if err != nil {
		result.Status = StatusFail
		result.Message = err.Error()
		return settle()
	}
	if c.Skip {
		result.Status = StatusSkip
		result.Message = "skip: true"
		return settle()
	}
	// exclude_distros: skip when any image distro tag intersects the exclusion list.
	if len(c.ExcludeDistros) > 0 {
		for _, imgTag := range pc.Distros() {
			if slices.Contains(c.ExcludeDistros, imgTag) {
				result.Status = StatusSkip
				result.Message = fmt.Sprintf("excluded on distro %q", imgTag)
				return settle()
			}
		}
	}
	// Context-vs-mode skip (the core VerbCatalog grammar, via the seam).
	if reason := pc.ContextSkipReason(c); reason != "" {
		result.Status = StatusSkip
		result.Message = reason
		return settle()
	}
	// Per-step VENUE dispatch — swap executor + resolver + image for this check only.
	if restore, failReason := pc.SwapVenue(c); failReason != "" {
		result.Status = StatusFail
		result.Message = failReason
		return settle()
	} else if restore != nil {
		defer restore()
	}

	// Expand variables in-place on a copy so repeated runs over the same check list don't
	// accumulate substitutions. The env overlays the scenario captures + ${HOST:…} onto the
	// resolver base.
	expanded := *c
	env := pc.EffectiveEnv()
	missing := ExpandOpVars(&expanded, env)
	if len(missing) > 0 {
		// An unresolved cross-deployment var (${HOST:…}) means the peer this probe targets is
		// UNREACHABLE — the probe's premise failed, so it FAILS (a SKIP there would be a fake
		// pass). Other unresolved vars stay a legitimate SKIP (a deploy-only var under build
		// scope, an unmounted volume — inputs that genuinely don't apply to this run).
		if hostMissing := FilterHostVars(missing); len(hostMissing) > 0 {
			result.Status = StatusFail
			result.Message = fmt.Sprintf("peer unreachable — unresolved cross-deployment variable(s): %s", strings.Join(hostMissing, ", "))
		} else {
			result.Status = StatusSkip
			result.Message = fmt.Sprintf("unresolved variables: %s", strings.Join(missing, ", "))
		}
		return settle()
	}

	// Verb dispatch, wrapped in the `eventually:` retry when requested.
	dispatch := func() CheckResult {
		// Per-probe never-hang: bound THIS attempt so a wedged probe is cancelled individually
		// and the pass continues. RunWithEventually calls dispatch once per attempt, so each
		// retry gets a FRESH bound; the author's timeout:/eventually: operate inside it.
		ctx, cancel := context.WithTimeout(ctx, pc.ProbeNeverHang(&expanded))
		defer cancel()
		var dr CheckResult
		// do-mode branch: a do:act state-provision verb executes its create/configure. Action
		// verbs (command/http/dbus/cdp/…) act in their own handler, so do:act there falls
		// through to the assert dispatch below (the handler IS the act).
		if pc.EffectiveDo(&expanded) == DoAct {
			if act, ok := pc.Verbs().RunProvisionAct(ctx, &expanded, kind); ok {
				return act
			}
		}
		if vr, ok := pc.Verbs().RunVerb(ctx, &expanded); ok {
			dr = vr
		} else {
			dr.Status = StatusSkip
			dr.Message = fmt.Sprintf("unknown verb %q", kind)
		}
		// If THIS attempt's per-attempt deadline fired, the probe ran too long — an
		// AUTHORITATIVE "too slow" failure, flagged so the killed-probe retry does not futilely
		// re-run a probe that will only re-hang and re-hit the same deadline.
		if ctx.Err() == context.DeadlineExceeded {
			dr.DeadlineExceeded = true
		}
		return dr
	}

	result = RunWithEventually(ctx, &expanded, dispatch)
	result.Op = c
	result.Verb = kind
	result.Elapsed = time.Since(start)
	// RunWithEventually sets TotalElapsed relative to its own start; prefer that for
	// multi-attempt cases. For single-attempt, Elapsed ≈ TotalElapsed.
	if result.TotalElapsed == 0 {
		result.TotalElapsed = result.Elapsed
	}
	return result
}

// RunPlan executes the flat plan in a LabelDescriptionSet (already collected +
// include-expanded + overlay-merged) against the plan context, returning per-step results.
//
// The context's mode selects which steps execute: VerifyOnly (check live / box) runs
// check:/agent-check: only; provision-and-verify (default) runs every step in order.
// include: steps never reach here (expanded at collect time); a residual one is a no-op skip.
// Agent steps route to the grader; run/check stamp the keyword-derived do-mode and dispatch
// through RunOne.
func RunPlan(ctx context.Context, pc PlanContext, set *LabelDescriptionSet, strict bool) []StepResult {
	if set == nil {
		return nil
	}
	var flat []flatStep
	for _, sec := range [][]LabeledDescription{set.Candy, set.Box, set.Deploy} {
		for _, ld := range sec {
			for i, s := range ld.Plan {
				flat = append(flat, flatStep{origin: ld.Origin, desc: ld.Description, idx: i, step: s})
			}
		}
	}

	planCtx := NewScenarioContext()
	orig := pc.Scenario()
	pc.SetScenario(planCtx)
	defer pc.SetScenario(orig)

	var out []StepResult
	for _, fs := range flat {
		stepID := EffectiveStepID(&fs.step, fs.origin, fs.idx)
		out = append(out, runUnit(ctx, pc, fs, planCtx, stepID, strict))
	}

	// Reap host-side background processes spawned by command: steps.
	for _, pid := range planCtx.SnapshotBackgrounds() {
		_ = sendSIGTERM(pid)
	}
	return out
}

// runUnit executes one plan step and returns its result.
func runUnit(ctx context.Context, pc PlanContext, fs flatStep, stepCtx *ScenarioContext, stepID string, strict bool) StepResult {
	step := fs.step
	sr := StepResult{
		Keyword: string(KeywordOf(&step)),
		Text:    step.KeywordText(),
		Origin:  fs.origin,
		StepID:  stepID,
	}

	// include: steps were spliced at collect time — a residual one is a no-op.
	if step.IsInclude() {
		sr.Result = CheckResult{Status: StatusSkip, Message: "include expanded at collect time"}
		return sr
	}

	// VerifyOnly: skip mutating steps (run:/agent-run:).
	if pc.VerifyOnly() && step.Mutates() {
		sr.Result = CheckResult{Status: StatusSkip, Message: "skipped — verify-only mode (mutating step)"}
		return sr
	}

	// feature-run (ADE acceptance "Run"): skip the DETERMINISTIC run: install-timeline steps
	// (Mutates but not an agent step). The install ran at image-build; re-executing it against
	// a built/deployed target is redundant and fails for build-context steps.
	if pc.SkipDeterministicRun() && step.Mutates() && !step.IsAgent() {
		sr.Result = CheckResult{Status: StatusSkip, Message: "skipped — run: install-timeline step (feature-run verifies, does not re-install)"}
		return sr
	}

	// Agent steps route to the grader (read-only for agent-check).
	if step.IsAgent() {
		if g := pc.Grader(); g != nil {
			sr.Result = g.Grade(ctx, GraderRequest{
				Description: fs.desc,
				Keyword:     string(KeywordOf(&step)),
				Text:        step.KeywordText(),
				ReadOnly:    !step.Mutates(),
			})
			return sr
		}
		status := StatusSkip
		msg := "agent step (no grader bound)"
		if strict {
			status = StatusFail
			msg = "agent step (no grader bound) — strict mode"
		}
		sr.Result = CheckResult{Status: status, Message: msg, Verb: "agent"}
		return sr
	}

	// run:/check: — stamp the keyword-derived do-mode + the owning entity's origin, then
	// dispatch to RunOne. The per-step Op.Origin is NOT baked into the OCI label, so it MUST be
	// re-stamped here from the flattened group origin — RunOne consumers rely on it.
	op := step.Op
	op.Origin = fs.origin
	op.IntentDo = string(StepDoMode(&step))
	stepCtx.CurrentStepID = stepID
	sr.Result = RunOne(ctx, pc, &op)
	return sr
}

// KeywordOf returns the populated step keyword, or "" when none is set.
func KeywordOf(s *spec.Step) spec.StepKeyword {
	if k, err := s.StepKind(); err == nil {
		return k
	}
	return ""
}

// sendSIGTERM sends SIGTERM to a host-side PID. Best-effort.
func sendSIGTERM(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if strings.Contains(err.Error(), "process already finished") ||
			strings.Contains(err.Error(), "no such process") {
			return nil
		}
		return err
	}
	return nil
}
