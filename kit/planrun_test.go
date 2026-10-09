package kit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/spec/spec"
)

// fakeVerbResolver returns a canned result for every verb, recording the last op it saw
// (both the resolved verb word and the full *spec.Op, e.g. for asserting Origin-stamping or
// post-expansion PluginInput values).
type fakeVerbResolver struct {
	result   spec.CheckResult
	known    bool
	actKnown bool
	lastVerb string
	lastOp   *spec.Op
}

func (f *fakeVerbResolver) RunVerb(_ context.Context, op *spec.Op) (spec.CheckResult, bool) {
	f.lastVerb, _ = op.Kind()
	f.lastOp = op
	return f.result, f.known
}
func (f *fakeVerbResolver) RunProvisionAct(_ context.Context, _ *spec.Op, verb string) (spec.CheckResult, bool) {
	return spec.CheckResult{Status: StatusPass, Message: "acted " + verb}, f.actKnown
}

// fakePlanContext is a minimal PlanContext for exercising the walk without a host Runner.
type fakePlanContext struct {
	distros    []string
	mode       RunMode
	verifyOnly bool
	skipRun    bool
	ctxSkip    string
	do         spec.DoMode
	env        map[string]string
	verbs      VerbResolver
	grader     StepGrader
	scenario   *ScenarioContext
	// classify is the host's answer to ClassifyUnresolved; nil ⇒ UnresolvedConditional, the
	// behaviour every test had before charly#865.
	classify func(op *spec.Op, name string) UnresolvedClass
}

func (c *fakePlanContext) ClassifyUnresolved(op *spec.Op, name string) UnresolvedClass {
	if c.classify == nil {
		return UnresolvedConditional
	}
	return c.classify(op, name)
}

func (c *fakePlanContext) Distros() []string                     { return c.distros }
func (c *fakePlanContext) Mode() RunMode                         { return c.mode }
func (c *fakePlanContext) VerifyOnly() bool                      { return c.verifyOnly }
func (c *fakePlanContext) SkipDeterministicRun() bool            { return c.skipRun }
func (c *fakePlanContext) ContextSkipReason(*spec.Op) string     { return c.ctxSkip }
func (c *fakePlanContext) EffectiveDo(*spec.Op) spec.DoMode      { return c.do }
func (c *fakePlanContext) EffectiveEnv() map[string]string       { return c.env }
func (c *fakePlanContext) ProbeNeverHang(*spec.Op) time.Duration { return time.Second }
func (c *fakePlanContext) SwapVenue(*spec.Op) (func(), string)   { return nil, "" }
func (c *fakePlanContext) Scenario() *ScenarioContext            { return c.scenario }
func (c *fakePlanContext) SetScenario(sc *ScenarioContext)       { c.scenario = sc }
func (c *fakePlanContext) Verbs() VerbResolver                   { return c.verbs }
func (c *fakePlanContext) Grader() StepGrader                    { return c.grader }

func pluginOp() *spec.Op { return &spec.Op{Plugin: "http", PluginInput: map[string]any{"http": "x"}} }

func TestRunOne_SkipTrue(t *testing.T) {
	pc := &fakePlanContext{verbs: &fakeVerbResolver{}}
	op := pluginOp()
	op.Skip = true
	if r := RunOne(context.Background(), pc, op); r.Status != StatusSkip || r.Message != "skip: true" {
		t.Fatalf("skip:true → %v %q, want StatusSkip 'skip: true'", r.Status, r.Message)
	}
}

func TestRunOne_ExcludeDistros(t *testing.T) {
	pc := &fakePlanContext{distros: []string{"fedora:43", "fedora"}, verbs: &fakeVerbResolver{}}
	op := pluginOp()
	op.ExcludeDistros = []string{"fedora"}
	if r := RunOne(context.Background(), pc, op); r.Status != StatusSkip {
		t.Fatalf("exclude_distros hit → %v, want StatusSkip", r.Status)
	}
}

func TestRunOne_ContextSkip(t *testing.T) {
	pc := &fakePlanContext{ctxSkip: "context [build] not active in live mode", verbs: &fakeVerbResolver{}}
	if r := RunOne(context.Background(), pc, pluginOp()); r.Status != StatusSkip || r.Message == "" {
		t.Fatalf("context skip → %v %q, want StatusSkip with reason", r.Status, r.Message)
	}
}

func TestRunOne_UnknownVerbSkips(t *testing.T) {
	pc := &fakePlanContext{env: map[string]string{}, verbs: &fakeVerbResolver{known: false}}
	if r := RunOne(context.Background(), pc, pluginOp()); r.Status != StatusSkip {
		t.Fatalf("unknown verb → %v %q, want StatusSkip", r.Status, r.Message)
	}
}

// deadVarOp builds an op whose `http` field references ${name}, with name absent from the env —
// i.e. expansion cannot resolve it.
func deadVarOp(name, intentDo string) *spec.Op {
	op := pluginOp()
	op.IntentDo = intentDo
	op.PluginInput = map[string]any{"http": "http://${" + name + "}/x"}
	return op
}

// TestRunOne_DeadAssertionFailsNotSkips is the regression guard for opencharly/charly#865: an
// unresolved name the HOST says no mode or scope can supply, on an ASSERT-ONLY step, is an
// authoring error and must FAIL. Delivering it as a skip is how a plan that quietly stopped
// asserting keeps reading green — and it is the exact case the issue measured (`CANDY_DIR`, a
// candy var that a `check:`'s env can never carry by construction).
func TestRunOne_DeadAssertionFailsNotSkips(t *testing.T) {
	pc := &fakePlanContext{
		env:      map[string]string{},
		verbs:    &fakeVerbResolver{},
		classify: func(*spec.Op, string) UnresolvedClass { return UnresolvedUnknown },
	}
	r := RunOne(context.Background(), pc, deadVarOp("CANDY_DIR", string(spec.DoAssert)))
	if r.Status != StatusFail {
		t.Fatalf("a dead assertion on an assert-only step → %v %q, want StatusFail", r.Status, r.Message)
	}
	if !strings.Contains(r.Message, "CANDY_DIR") {
		t.Errorf("the failure must NAME the unresolvable variable; got %q", r.Message)
	}
}

// TestRunOne_ConditionalUnresolvedStillSkips is the other half of the contract: a name the host
// CAN supply in another mode or scope (a deploy-only var under build scope, an unmounted volume)
// keeps skipping. Without this, the fix would have been "fail on any unresolvable var", which
// would turn every legitimately-inapplicable step red.
func TestRunOne_ConditionalUnresolvedStillSkips(t *testing.T) {
	pc := &fakePlanContext{
		env:      map[string]string{},
		verbs:    &fakeVerbResolver{},
		classify: func(*spec.Op, string) UnresolvedClass { return UnresolvedConditional },
	}
	if r := RunOne(context.Background(), pc, deadVarOp("DEPLOY_ONLY", string(spec.DoAssert))); r.Status != StatusSkip {
		t.Fatalf("a var the host can supply elsewhere → %v %q, want StatusSkip", r.Status, r.Message)
	}
}

// TestRunOne_UnknownUnresolvedOnMutatingStepStillSkips: even a name nothing declares stays a skip
// on a MUTATING step, whose scope may legitimately supply it. The assert-only restriction is what
// makes the classification decidable rather than a guess.
func TestRunOne_UnknownUnresolvedOnMutatingStepStillSkips(t *testing.T) {
	pc := &fakePlanContext{
		env:      map[string]string{},
		verbs:    &fakeVerbResolver{},
		classify: func(*spec.Op, string) UnresolvedClass { return UnresolvedUnknown },
	}
	if r := RunOne(context.Background(), pc, deadVarOp("CANDY_DIR", string(spec.DoAct))); r.Status != StatusSkip {
		t.Fatalf("a mutating step → %v %q, want StatusSkip", r.Status, r.Message)
	}
}

// TestRunOne_HostVarStillFailsUnchanged pins that the ${HOST:…} arm — peer unreachable — is
// untouched by the new classification and is still decided FIRST.
func TestRunOne_HostVarStillFailsUnchanged(t *testing.T) {
	pc := &fakePlanContext{
		env:      map[string]string{},
		verbs:    &fakeVerbResolver{},
		classify: func(*spec.Op, string) UnresolvedClass { return UnresolvedConditional },
	}
	r := RunOne(context.Background(), pc, deadVarOp(HostVar+":peer", string(spec.DoAssert)))
	if r.Status != StatusFail || !strings.Contains(r.Message, "peer unreachable") {
		t.Fatalf("an unresolved ${HOST:…} → %v %q, want StatusFail naming the unreachable peer", r.Status, r.Message)
	}
}

func TestRunOne_DispatchPass(t *testing.T) {
	vr := &fakeVerbResolver{result: spec.CheckResult{Status: StatusPass, Message: "ok"}, known: true}
	pc := &fakePlanContext{env: map[string]string{}, verbs: vr}
	r := RunOne(context.Background(), pc, pluginOp())
	if r.Status != StatusPass {
		t.Fatalf("dispatch → %v, want StatusPass", r.Status)
	}
	if vr.lastVerb != "plugin" {
		t.Fatalf("resolver saw verb %q, want plugin", vr.lastVerb)
	}
	if r.Verb != "plugin" || r.Op == nil {
		t.Fatalf("result not stamped with Op+Verb: %+v", r)
	}
}

func TestRunOne_ActDispatch(t *testing.T) {
	vr := &fakeVerbResolver{actKnown: true, known: true, result: spec.CheckResult{Status: StatusPass}}
	pc := &fakePlanContext{env: map[string]string{}, do: spec.DoAct, verbs: vr}
	r := RunOne(context.Background(), pc, pluginOp())
	if r.Status != StatusPass || r.Message != "acted plugin" {
		t.Fatalf("do:act → %v %q, want the provision-act result", r.Status, r.Message)
	}
}

// stubGrader records the request it was handed.
type stubGrader struct{ got GraderRequest }

func (g *stubGrader) Grade(_ context.Context, req GraderRequest) CheckResult {
	g.got = req
	return CheckResult{CheckResult: spec.CheckResult{Status: StatusPass, Message: "graded"}}
}

func TestRunPlan_VerifyOnlySkipsMutating(t *testing.T) {
	vr := &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}
	pc := &fakePlanContext{env: map[string]string{}, verifyOnly: true, verbs: vr}
	set := &LabelDescriptionSet{Candy: []LabeledDescription{{
		Origin: "candy:x",
		Plan: []spec.Step{
			{Run: "install it", Op: *pluginOp()},  // mutating → skipped
			{Check: "verify it", Op: *pluginOp()}, // verify → runs
		},
	}}}
	out := RunPlan(context.Background(), pc, set, false)
	if len(out) != 2 {
		t.Fatalf("RunPlan → %d results, want 2", len(out))
	}
	if out[0].Result.Status != StatusSkip {
		t.Errorf("verify-only run: step → %v, want StatusSkip", out[0].Result.Status)
	}
	if out[1].Result.Status != StatusPass {
		t.Errorf("check: step → %v, want StatusPass", out[1].Result.Status)
	}
}

func TestRunPlan_AgentStepGraded(t *testing.T) {
	g := &stubGrader{}
	pc := &fakePlanContext{env: map[string]string{}, verbs: &fakeVerbResolver{}, grader: g}
	set := &LabelDescriptionSet{Candy: []LabeledDescription{{
		Origin:      "candy:x",
		Description: "the goal",
		Plan:        []spec.Step{{AgentCheck: "assess it"}},
	}}}
	out := RunPlan(context.Background(), pc, set, false)
	if len(out) != 1 || out[0].Result.Status != StatusPass || out[0].Result.Message != "graded" {
		t.Fatalf("agent step → %+v, want graded pass", out)
	}
	if g.got.Description != "the goal" || !g.got.ReadOnly {
		t.Errorf("grader request = %+v, want desc 'the goal' + ReadOnly (agent-check)", g.got)
	}
}

func TestRunPlan_AgentStepNoGrader_StrictFails(t *testing.T) {
	pc := &fakePlanContext{env: map[string]string{}, verbs: &fakeVerbResolver{}}
	set := &LabelDescriptionSet{Candy: []LabeledDescription{{
		Origin: "candy:x",
		Plan:   []spec.Step{{AgentCheck: "assess it"}},
	}}}
	strict := RunPlan(context.Background(), pc, set, true)
	if strict[0].Result.Status != StatusFail {
		t.Errorf("strict + no grader → %v, want StatusFail", strict[0].Result.Status)
	}
	lax := RunPlan(context.Background(), pc, set, false)
	if lax[0].Result.Status != StatusSkip {
		t.Errorf("lax + no grader → %v, want StatusSkip", lax[0].Result.Status)
	}
}
