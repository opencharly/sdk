package kit

import (
	"context"
	"sync"
)

// planrun_staged.go — the STAGE-GROUPED + CONCURRENT bed-member walk (Cutover C
// tasks 5-7). A bed's plan is the UNION of its members' plans. The walk lives
// here (sdk/kit) so ANY plugin candy that runs a bed plan drives the SAME
// semantics the check engine uses — one walk, R3.
//
// Execution model (task 5):
//
//   - When ANY step of the bed carries a stage, steps are grouped by stage and
//     stage groups execute in FIRST-SEEN order (the order stage names first
//     appear across members in authored order — recorded at load, task 4).
//   - WITHIN a stage, steps on DIFFERENT members run CONCURRENTLY, each on
//     that member's OWN independent Runner instance (A4 — never share one
//     Runner); same-member steps in the stage keep authored order.
//   - `parallel:` does NOT interact with stages — stages dominate.
//   - WITHOUT stages: today's sequential per-member walk, plus
//     whole-member-plan concurrency when `parallel: true`.
//
// Result collection (task 6): per-member result slices; every append into the
// shared ordered summary serializes under ONE mutex (the A4 fix — concurrent
// member goroutines never race on the shared output); the final order is
// venue-keyed DETERMINISTIC (members in authored order; within a member,
// steps in authored order), never completion-time.
//
// Failure semantics (task 7): a member step failure does NOT stop other
// members — every stage DRAINS (WaitGroup barriers wait for all members of a
// stage before the next stage starts); per-member verdicts land in the
// results; nothing here ever cancels a sibling member on another member's
// failure. Tear-down of every member on every terminal path is the caller's
// bed runner (plugin-check's bed run already tears down members via
// deploykit.TearDownMembers on every path); the walk only guarantees the drain
// semantics a tear-down can rely on.

// MemberRun is ONE member's contribution to a bed walk: its already-collected
// plan set plus an INDEPENDENT Runner (PlanContext) for that member. A4 — the
// walk NEVER shares one Runner across members; every concurrently-executing
// step of a different member runs on that member's own Runner.
type MemberRun struct {
	// Origin is the member's origin key ("deploy:<name>" / "vm:<name>"/…),
	// used for deterministic result ordering (venue-keyed).
	Origin string
	// Set is the member's collected plan (include-expanded, overlay-merged).
	Set *LabelDescriptionSet
	// Runner is the member's OWN independent PlanContext. Never nil for a
	// member with steps; the walk calls Scenario/SetScenario on it (one
	// scenario per member for the whole walk).
	Runner PlanContext
}

// memberFlat is one member's flattened plan steps (authored order).
type memberFlat struct {
	flat []flatStep
}

// RunPlanStaged executes a bed's member plans with stage grouping + member
// concurrency per the model above. members are in authored order; strict
// selects the agent-grader no-grader disposition (see RunPlan).
func RunPlanStaged(ctx context.Context, members []MemberRun, parallel bool, strict bool) []StepResult {
	// Flatten every member's set into flat steps, recording stage first-seen
	// order as authored (load-time order, task 4).
	flats := make([]memberFlat, 0, len(members))
	var stageOrder []string
	seenStage := map[string]bool{}
	hasStages := false
	for mi := range members {
		set := members[mi].Set
		if set == nil {
			continue
		}
		var flat []flatStep
		for _, sec := range [][]LabeledDescription{set.Candy, set.Box, set.Deploy} {
			for _, ld := range sec {
				for i, s := range ld.Plan {
					fs := flatStep{origin: ld.Origin, desc: ld.Description, idx: i, step: s}
					flat = append(flat, fs)
					if st := s.Stage; st != "" {
						hasStages = true
						if !seenStage[st] {
							seenStage[st] = true
							stageOrder = append(stageOrder, st)
						}
					}
				}
			}
		}
		flats = append(flats, memberFlat{flat: flat})
	}

	// Stages dominate `parallel:` — execute the stage-grouped walk.
	if hasStages {
		return runStagedGroups(ctx, members, flats, stageOrder, strict)
	}
	// No stages: sequential per-member walk, plus whole-member concurrency when
	// `parallel: true`.
	if parallel {
		return runParallelMembers(ctx, members, flats, strict)
	}
	return runSequentialMembers(ctx, members, flats, strict)
}

// runSequentialMembers executes each member's plan in authored order — today's
// walk (one scenario per member, restored after). No concurrency: same order,
// same semantics as calling RunPlan per member.
func runSequentialMembers(ctx context.Context, members []MemberRun, flats []memberFlat, strict bool) []StepResult {
	out := make([]StepResult, 0, totalFlatSteps(flats))
	for mi := range flats {
		out = append(out, runOneMemberSteps(ctx, members[mi].Runner, flats[mi].flat, strict)...)
	}
	return out
}

// totalFlatSteps sums the flat step counts across members for result-slice
// preallocation.
func totalFlatSteps(flats []memberFlat) int {
	n := 0
	for _, mf := range flats {
		n += len(mf.flat)
	}
	return n
}

// runParallelMembers executes each member's WHOLE plan concurrently on that
// member's own Runner (whole-member-plan concurrency, `parallel: true`). Per
// member: one scenario for the whole plan (like RunPlan). Results land in
// per-member slices whose shared summary append serializes under one mutex;
// the final order is member-authored (venue-keyed deterministic).
func runParallelMembers(ctx context.Context, members []MemberRun, flats []memberFlat, strict bool) []StepResult {
	col := newResultCollector(len(members))
	var wg sync.WaitGroup
	for mi := range flats {
		if members[mi].Runner == nil {
			continue
		}
		wg.Add(1)
		go func(mi int) {
			defer wg.Done()
			res := runOneMemberSteps(ctx, members[mi].Runner, flats[mi].flat, strict)
			col.set(mi, res)
		}(mi)
	}
	wg.Wait() // drain — every member finishes; failures never cancel siblings
	return col.ordered()
}

// runStagedGroups groups all members' steps by stage (first-seen order) and
// executes the stage groups in order. Within a stage, each member's steps in
// that stage run sequentially on that member's OWN Runner, CONCURRENTLY across
// members (goroutines + WaitGroup barrier drain). Same-member steps keep
// authored order; the first-seen stage order is deterministic.
func runStagedGroups(ctx context.Context, members []MemberRun, flats []memberFlat, stageOrder []string, strict bool) []StepResult {
	// index: memberIdx -> stageIdx -> member's flat steps in that stage, authored order.
	memberStageSteps := make([][][]flatStep, len(flats))
	for mi := range flats {
		memberStageSteps[mi] = make([][]flatStep, len(stageOrder))
	}
	for mi, mf := range flats {
		for _, fs := range mf.flat {
			st := fs.step.Stage
			si := 0
			if st != "" {
				for i, name := range stageOrder {
					if name == st {
						si = i
						break
					}
				}
			}
			// An unstaged step (all-or-nothing is a LOAD-time validation, task 4;
			// a walk should never see a mixed bed) executes in the FIRST stage
			// group in authored position — a loader gap can never drop a step.
			memberStageSteps[mi][si] = append(memberStageSteps[mi][si], fs)
		}
	}

	col := newResultCollector(len(members))
	for si := range stageOrder {
		var wg sync.WaitGroup
		for mi := range memberStageSteps {
			steps := memberStageSteps[mi][si]
			if len(steps) == 0 || members[mi].Runner == nil {
				continue
			}
			wg.Add(1)
			go func(mi int, steps []flatStep) {
				defer wg.Done()
				res := runOneMemberSteps(ctx, members[mi].Runner, steps, strict)
				col.appendMemberStage(mi, res)
			}(mi, steps)
		}
		wg.Wait() // stage DRAIN — all members of this stage complete before the next starts
	}
	return col.ordered()
}

// runOneMemberSteps executes one member's flat steps in authored order against
// the member's own Runner, with ONE scenario for the whole run (mirrors
// RunPlan's scenario lifecycle: fresh context, restored after) and the
// background reaping RunPlan performs at the end of a plan walk.
func runOneMemberSteps(ctx context.Context, pc PlanContext, flat []flatStep, strict bool) []StepResult {
	if pc == nil || len(flat) == 0 {
		return nil
	}
	planCtx := NewScenarioContext()
	orig := pc.Scenario()
	pc.SetScenario(planCtx)
	defer pc.SetScenario(orig)

	out := make([]StepResult, 0, len(flat))
	for _, fs := range flat {
		stepID := EffectiveStepID(&fs.step, fs.origin, fs.idx)
		out = append(out, runUnit(ctx, pc, fs, planCtx, stepID, strict))
	}
	for _, pid := range planCtx.SnapshotBackgrounds() {
		_ = sendSIGTERM(pid)
	}
	return out
}

// resultCollector is the concurrency-safe result assembly (task 6): one mutex
// serializes every append into the shared per-member summaries; the final
// []StepResult is assembled in venue-keyed deterministic order (member order,
// authored step order within a member).
type resultCollector struct {
	mu      sync.Mutex
	members [][]StepResult
}

func newResultCollector(n int) *resultCollector {
	return &resultCollector{members: make([][]StepResult, n)}
}

// set stores a member's whole-plan results (sequential/parallel mode). One
// member, one call.
func (c *resultCollector) set(mi int, res []StepResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mi < len(c.members) {
		c.members[mi] = res
	}
}

// appendMemberStage appends one member's block of results for one stage.
// Stages execute in order, so per-member appends land in stage order — the
// mutex serializes the shared-summary append (the A4 fix).
func (c *resultCollector) appendMemberStage(mi int, res []StepResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mi < len(c.members) {
		c.members[mi] = append(c.members[mi], res...)
	}
}

// ordered assembles the venue-keyed deterministic summary: members in authored
// order; within a member, steps in authored order (staged runs preserve order
// because per-member stage appends land sequentially in stage order).
func (c *resultCollector) ordered() []StepResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []StepResult
	for _, memberRes := range c.members {
		out = append(out, memberRes...)
	}
	return out
}
