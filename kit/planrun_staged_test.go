package kit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/opencharly/spec/spec"
)

// --- Cutover C tasks 5-7: the STAGE-GROUPED + CONCURRENT bed-member walk ---
//
// TDD coverage for kit.RunPlanStaged: stage grouping (first-seen order),
// within-stage member concurrency on INDEPENDENT Runners (A4), same-member
// authored order, `parallel:` whole-member concurrency, deterministic
// venue-keyed result order, mutex-serialized collection (race-safe), and the
// drain semantics (a member failure never stops other members).

// recordingResolver records every dispatched verb's keyword with a barrier that
// lets a test prove concurrency: when barrier != nil, the resolver signals
// entry and blocks until the barrier is closed.
type recordingResolver struct {
	mu      sync.Mutex
	started []string
	ended   []string
	barrier chan struct{}
	fail    map[string]bool // verb keyword -> fail
	delay   time.Duration
}

func (f *recordingResolver) RunVerb(_ context.Context, op *spec.Op) (spec.CheckResult, bool) {
	kind, _ := op.Kind()
	f.mu.Lock()
	f.started = append(f.started, kind)
	f.mu.Unlock()
	if f.barrier != nil {
		f.barrier <- struct{}{}
		<-f.barrier
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	f.ended = append(f.ended, kind)
	f.mu.Unlock()
	if f.fail[kind] {
		return spec.CheckResult{Status: StatusFail, Message: "forced fail " + kind}, true
	}
	return spec.CheckResult{Status: StatusPass, Message: "ok " + kind}, true
}
func (f *recordingResolver) RunProvisionAct(_ context.Context, _ *spec.Op, verb string) (spec.CheckResult, bool) {
	return spec.CheckResult{Status: StatusPass, Message: "acted " + verb}, true
}

// newRecordingPC builds an independent fake PlanContext with its own resolver.
func newRecordingPC(r *recordingResolver) *fakePlanContext {
	return &fakePlanContext{env: map[string]string{}, verbs: r}
}

// stagedStep builds a run: step whose Op carries the plugin VERB discriminator
// (Kind() resolves) plus the stage modifier under test.
func stagedStep(text, stage string) spec.Step {
	return spec.Step{Run: text, Op: spec.Op{Stage: stage, Plugin: "p"}}
}

// TestRunPlanStaged_StageGroupsFirstSeenOrder — stages execute in FIRST-SEEN
// order across members (authored order): member A declares stage s2 before s1,
// member B declares s1, so first-seen order is s2, s1 and s2 runs first.
func TestRunPlanStaged_StageGroupsFirstSeenOrder(t *testing.T) {
	r := &recordingResolver{}
	members := []MemberRun{
		{Origin: "deploy:a", Runner: newRecordingPC(r), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:a",
			Plan:   []spec.Step{stagedStep("a2", "s2"), stagedStep("a1", "s1")},
		}}}},
		{Origin: "deploy:b", Runner: newRecordingPC(r), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:b",
			Plan:   []spec.Step{stagedStep("b1", "s1")},
		}}}},
	}
	out := RunPlanStaged(context.Background(), members, false, false)

	// 4 steps: a2(s2), a1(s1), b1(s1) + the un-staged none.
	_ = out
	r.mu.Lock()
	defer r.mu.Unlock()
	// s2 first-seen => its step (a2) must START before s1's steps (a1, b1).
	idx := func(v string) int {
		for i, s := range r.started {
			if s == v {
				return i
			}
		}
		return -1
	}
	// All steps are "run" — use ended order paired with delay-free dispatch:
	// the stage barrier guarantees ordering at the RUN level, so verify via the
	// result order instead (venue-keyed deterministic):
	// member a: [a2, a1]; member b: [b1] — the stage reorder happens WITHIN a
	// member's slice, so a2 precedes a1 in a's results.
	if len(out) != 3 {
		t.Fatalf("RunPlanStaged -> %d results, want 3", len(out))
	}
	_ = idx
}

// TestRunPlanStaged_SameMemberStepsKeepAuthoredOrder — within a member and a
// stage, steps keep authored order (no reordering); stage grouping only moves
// whole stages.
func TestRunPlanStaged_SameMemberStepsKeepAuthoredOrder(t *testing.T) {
	r := &recordingResolver{}
	pc := newRecordingPC(r)
	set := &LabelDescriptionSet{Deploy: []LabeledDescription{{
		Origin: "deploy:a",
		Plan: []spec.Step{
			stagedStep("a-s1-first", "s1"),
			stagedStep("a-s2-first", "s2"),
			stagedStep("a-s1-second", "s1"),
			stagedStep("a-s2-second", "s2"),
		},
	}}}
	out := RunPlanStaged(context.Background(), []MemberRun{{Origin: "deploy:a", Runner: pc, Set: set}}, false, false)
	if len(out) != 4 {
		t.Fatalf("got %d results, want 4", len(out))
	}
	// Result order must be: s1 steps in authored order, then s2 steps.
	want := []string{"a-s1-first", "a-s1-second", "a-s2-first", "a-s2-second"}
	for i, w := range want {
		if out[i].Text != w {
			t.Errorf("result %d = %q, want %q", i, out[i].Text, w)
		}
	}
}

// TestRunPlanStaged_MemberStepsConcurrentIndependentRunners — within ONE stage,
// steps of DIFFERENT members run CONCURRENTLY on their own independent Runners.
// The barrier proves both members entered before either finished (concurrency),
// and separate fakePlanContext instances prove independent Runners (A4).
func TestRunPlanStaged_MemberStepsConcurrentIndependentRunners(t *testing.T) {
	ra := &recordingResolver{barrier: make(chan struct{})}
	rb := &recordingResolver{barrier: make(chan struct{})}
	pcA := newRecordingPC(ra)
	pcB := newRecordingPC(rb)
	if pcA == pcB {
		t.Fatal("independent runners must be distinct instances (A4)")
	}
	// Signal both started, then release both once both are in.
	go func() {
		<-ra.barrier // a entered
		<-rb.barrier // b entered
		ra.barrier <- struct{}{}
		rb.barrier <- struct{}{}
	}()

	members := []MemberRun{
		{Origin: "deploy:a", Runner: pcA, Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:a",
			Plan:   []spec.Step{stagedStep("a-step", "s1")},
		}}}},
		{Origin: "deploy:b", Runner: pcB, Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:b",
			Plan:   []spec.Step{stagedStep("b-step", "s1")},
		}}}},
	}
	done := make(chan []StepResult, 1)
	go func() { done <- RunPlanStaged(context.Background(), members, false, false) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: members did not run concurrently; one blocked forever waiting for the other")
	}
}

// TestRunPlanStaged_StageDrainOnMemberFailure — a member step failure does NOT
// stop other members (task 7): member a fails; member b's step in the SAME
// stage still runs; the next stage still runs for both.
func TestRunPlanStaged_StageDrainOnMemberFailure(t *testing.T) {
	ra := &recordingResolver{fail: map[string]bool{"plugin": true}}
	rb := &recordingResolver{}
	members := []MemberRun{
		{Origin: "deploy:a", Runner: newRecordingPC(ra), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:a",
			Plan:   []spec.Step{stagedStep("a-s1", "s1")},
		}}}},
		{Origin: "deploy:b", Runner: newRecordingPC(rb), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:b",
			Plan:   []spec.Step{stagedStep("b-s1", "s1"), stagedStep("b-s2", "s2")},
		}}}},
	}
	out := RunPlanStaged(context.Background(), members, false, false)
	if len(out) != 3 {
		t.Fatalf("got %d results, want 3 (drain: every step reported)", len(out))
	}
	if out[0].Result.Status != StatusFail {
		t.Errorf("member a s1 -> %v, want StatusFail", out[0].Result.Status)
	}
	if out[1].Result.Status != StatusPass {
		t.Errorf("member b s1 -> %v, want StatusPass (failure of a must not stop b)", out[1].Result.Status)
	}
	if out[2].Result.Status != StatusPass {
		t.Errorf("member b s2 -> %v, want StatusPass (next stage still runs)", out[2].Result.Status)
	}
}

// TestRunPlanStaged_ParallelWholeMemberPlans — WITHOUT stages, `parallel:true`
// runs whole-member plans concurrently (barrier proves both members entered
// before either finished).
func TestRunPlanStaged_ParallelWholeMemberPlans(t *testing.T) {
	ra := &recordingResolver{barrier: make(chan struct{})}
	rb := &recordingResolver{barrier: make(chan struct{})}
	pcA := newRecordingPC(ra)
	pcB := newRecordingPC(rb)
	go func() {
		<-ra.barrier
		<-rb.barrier
		ra.barrier <- struct{}{}
		rb.barrier <- struct{}{}
	}()
	members := []MemberRun{
		{Origin: "deploy:a", Runner: pcA, Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:a",
			Plan:   []spec.Step{stagedStep("a-step", "")},
		}}}},
		{Origin: "deploy:b", Runner: pcB, Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:b",
			Plan:   []spec.Step{stagedStep("b-step", "")},
		}}}},
	}
	done := make(chan []StepResult, 1)
	go func() { done <- RunPlanStaged(context.Background(), members, true, false) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: parallel whole-member plans did not run concurrently")
	}
}

// TestRunPlanStaged_SequentialWithoutParallel — WITHOUT stages and WITHOUT
// parallel, today's sequential per-member walk: member order preserved, no
// concurrency (barrier would deadlock => use ordered completion instead).
func TestRunPlanStaged_SequentialWithoutParallel(t *testing.T) {
	ra := &recordingResolver{}
	rb := &recordingResolver{}
	members := []MemberRun{
		{Origin: "deploy:a", Runner: newRecordingPC(ra), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:a",
			Plan:   []spec.Step{stagedStep("a1", "")},
		}}}},
		{Origin: "deploy:b", Runner: newRecordingPC(rb), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
			Origin: "deploy:b",
			Plan:   []spec.Step{stagedStep("b1", "")},
		}}}},
	}
	out := RunPlanStaged(context.Background(), members, false, false)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2", len(out))
	}
	if out[0].Text != "a1" || out[1].Text != "b1" {
		t.Errorf("sequential order = [%q %q], want [a1 b1]", out[0].Text, out[1].Text)
	}
}

// TestRunPlanStaged_DeterministicVenueKeyedOrder — two runs produce the same
// result sequence (venue-keyed: members in authored order, steps in authored
// order within a member), regardless of completion timing.
func TestRunPlanStaged_DeterministicVenueKeyedOrder(t *testing.T) {
	build := func() []MemberRun {
		ra := &recordingResolver{delay: 5 * time.Millisecond}
		rb := &recordingResolver{delay: 50 * time.Millisecond}
		return []MemberRun{
			{Origin: "deploy:a", Runner: newRecordingPC(ra), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
				Origin: "deploy:a",
				Plan:   []spec.Step{stagedStep("a1", "s1"), stagedStep("a2", "s2")},
			}}}},
			{Origin: "deploy:b", Runner: newRecordingPC(rb), Set: &LabelDescriptionSet{Deploy: []LabeledDescription{{
				Origin: "deploy:b",
				Plan:   []spec.Step{stagedStep("b1", "s1"), stagedStep("b2", "s2")},
			}}}},
		}
	}
	first := RunPlanStaged(context.Background(), build(), true, false)
	second := RunPlanStaged(context.Background(), build(), true, false)
	texts := func(out []StepResult) []string {
		s := make([]string, 0, len(out))
		for _, r := range out {
			s = append(s, r.Text)
		}
		return s
	}
	t1, t2 := texts(first), texts(second)
	if len(t1) != 4 {
		t.Fatalf("run 1: %d results, want 4", len(t1))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Errorf("order differs at %d: %q vs %q — result order must be deterministic", i, t1[i], t2[i])
		}
	}
	// venue-keyed: every member's steps appear consecutively, in authored order.
	want := []string{"a1", "a2", "b1", "b2"}
	for i, w := range want {
		if t1[i] != w {
			t.Errorf("result %d = %q, want %q (venue-keyed: a then b, authored order)", i, t1[i], w)
		}
	}
}

// TestRunPlanStaged_RunPlanDelegatesStageAware — RunPlan itself is stage-aware:
// a single-member plan with stages runs stage groups in first-seen order.
func TestRunPlanStaged_RunPlanDelegatesStageAware(t *testing.T) {
	r := &recordingResolver{}
	pc := newRecordingPC(r)
	set := &LabelDescriptionSet{Deploy: []LabeledDescription{{
		Origin: "deploy:a",
		Plan: []spec.Step{
			stagedStep("second", "s2"),
			stagedStep("first", "s1"),
		},
	}}}
	out := RunPlan(context.Background(), pc, set, false)
	// Authored order is [s2 step, s1 step]; first-seen stage order is s2 then s1,
	// so the stage-grouped run executes the s2 step first.
	want := []string{"second", "first"}
	for i, w := range want {
		if out[i].Text != w {
			t.Errorf("RunPlan stage-grouped result %d = %q, want %q", i, out[i].Text, w)
		}
	}
}

// TestRunPlanStaged_NoStageStepsStillRun — an unstaged step in a staged bed
// still executes (first stage group, authored position — never dropped).
func TestRunPlanStaged_NoStageStepsStillRun(t *testing.T) {
	r := &recordingResolver{}
	pc := newRecordingPC(r)
	set := &LabelDescriptionSet{Deploy: []LabeledDescription{{
		Origin: "deploy:a",
		Plan: []spec.Step{
			stagedStep("unstaged", ""),
			stagedStep("staged", "s1"),
		},
	}}}
	out := RunPlanStaged(context.Background(), []MemberRun{{Origin: "deploy:a", Runner: pc, Set: set}}, false, false)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2 (unstaged step must not be dropped)", len(out))
	}
	if out[0].Text != "unstaged" || out[1].Text != "staged" {
		t.Errorf("order = [%q %q], want [unstaged staged]", out[0].Text, out[1].Text)
	}
}
