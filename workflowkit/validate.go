package workflowkit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/opencharly/spec/spec"
)

// validate.go — the workflow IR validator. It is the ONE gate every engine
// (plugin-lobster today, any future engine) runs before lowering: the authored
// `kind: pipeline` body must be well-formed, self-consistent, and expressible in
// lobster's grammar, so a workflow never fails halfway through a live run.

// execArms are the mutually exclusive execution arms of a step/sub-step. Exactly one
// must be set: a step that sets none does nothing, a step that sets two is ambiguous.
//
// `steps:` is deliberately NOT here: it is the for_each BODY, not an arm — `spec` groups
// it with the other for_each companions ("--- for_each companions (meaningful only
// alongside for_each) ---", PipelineStepBase). Counting it as an arm made every valid
// for_each step set TWO arms and be rejected. A `steps:` with no `for_each` is caught by
// the stray-companion rule below instead.
var execArms = []string{"run", "pipeline", "workflow", "parallel", "for_each", "input", "plan", "charly", "approval"}

// waitModes is the closed `parallel.wait` vocabulary (upstream lobster).
var waitModes = []string{"", "all", "any"}

// ValidatePipeline validates ONE authored pipeline: the exec-arm XOR, the co-field
// rules, unique step ids (parallel branches and for_each `steps:` included), step
// references to EARLIER steps only, the lobster expression grammar on every `when:`,
// and `item_var` ≠ `index_var`. It returns a joined error naming every problem found
// (never just the first), so an author fixes one round.
func ValidatePipeline(p *spec.Pipeline) error {
	if p == nil {
		return errors.New("pipeline is nil")
	}
	var errs []error
	seen := map[string]string{} // id -> where declared, for the uniqueness diagnostic
	defined := map[string]bool{}

	for i := range p.Steps {
		s := &p.Steps[i]
		where := fmt.Sprintf("steps[%d]", i)
		if s.Id == "" {
			errs = append(errs, fmt.Errorf("%s: id is required", where))
		} else {
			where = fmt.Sprintf("step %q", s.Id)
			if prev, dup := seen[s.Id]; dup {
				errs = append(errs, fmt.Errorf("%s: duplicate step id (already declared at %s)", where, prev))
			} else {
				seen[s.Id] = where
			}
		}
		errs = append(errs, validateStepBase(stepView{
			id: s.Id, when: s.When, stdin: s.Stdin, forEach: s.ForEach,
			itemVar: s.ItemVar, indexVar: s.IndexVar, batchSize: s.BatchSize, pauseMs: s.PauseMs,
			run: s.Run, pipeline: s.Pipeline, workflow: s.Workflow, charly: len(s.Charly) > 0,
			plan: len(s.Plan) > 0, planRefs: planRefNames(s.Plan), steps: len(s.Steps) > 0, input: nonZeroInput(s.Input),
			approval: s.Approval != nil, parallelBranches: len(s.Parallel.Branches),
			parallelWait: s.Parallel.Wait, refsAmong: defined,
		}, where)...)

		for j := range s.Parallel.Branches {
			b := &s.Parallel.Branches[j]
			bwhere := fmt.Sprintf("%s.parallel.branches[%d]", where, j)
			if b.Id == "" {
				errs = append(errs, fmt.Errorf("%s: id is required", bwhere))
			} else {
				bwhere = fmt.Sprintf("%s branch %q", where, b.Id)
				if prev, dup := seen[b.Id]; dup {
					errs = append(errs, fmt.Errorf("%s: duplicate step id (already declared at %s)", bwhere, prev))
				} else {
					seen[b.Id] = bwhere
				}
			}
			errs = append(errs, validateStepBase(stepView{
				id: b.Id, when: b.When, stdin: b.Stdin,
				run: b.Run, pipeline: b.Pipeline, plan: len(b.Plan) > 0, planRefs: planRefNames(b.Plan), charly: len(b.Charly) > 0,
				steps: false, input: false, approval: false, refsAmong: defined,
			}, bwhere)...)
		}

		for j := range s.Steps {
			b := &s.Steps[j]
			bwhere := fmt.Sprintf("%s.steps[%d]", where, j)
			if b.Id == "" {
				errs = append(errs, fmt.Errorf("%s: id is required", bwhere))
			} else {
				bwhere = fmt.Sprintf("%s sub-step %q", where, b.Id)
				if prev, dup := seen[b.Id]; dup {
					errs = append(errs, fmt.Errorf("%s: duplicate step id (already declared at %s)", bwhere, prev))
				} else {
					seen[b.Id] = bwhere
				}
			}
			errs = append(errs, validateStepBase(stepView{
				id: b.Id, when: b.When, stdin: b.Stdin,
				run: b.Run, pipeline: b.Pipeline, plan: len(b.Plan) > 0, planRefs: planRefNames(b.Plan), charly: len(b.Charly) > 0,
				steps: false, input: false, approval: false, refsAmong: defined,
			}, bwhere)...)
		}

		if s.Id != "" {
			defined[s.Id] = true
		}
	}

	return errors.Join(errs...)
}

// stepView is the common shape of PipelineStep / PipelineSubStep for validation, so
// the rules live once (R3) instead of being copy-pasted per type.
type stepView struct {
	id, when, stdin, forEach     string
	itemVar, indexVar            string
	batchSize, pauseMs           int64
	run, pipeline, workflow      string
	charly                       bool
	plan, steps, input, approval bool
	planRefs                     []string
	parallelBranches             int
	parallelWait                 string
	refsAmong                    map[string]bool
}

func validateStepBase(v stepView, where string) []error {
	var errs []error

	// exactly one exec arm.
	arms := map[string]bool{
		"run":      v.run != "",
		"pipeline": v.pipeline != "",
		"workflow": v.workflow != "",
		"parallel": v.parallelBranches > 0,
		"for_each": v.forEach != "",
		"input":    v.input,
		"plan":     v.plan,
		"charly":   v.charly,
		"approval": v.approval,
	}
	var set []string
	for _, a := range execArms {
		if arms[a] {
			set = append(set, a)
		}
	}
	switch len(set) {
	case 1:
	case 0:
		errs = append(errs, fmt.Errorf("%s: no execution arm — set exactly one of %s", where, strings.Join(execArms, "/")))
	default:
		errs = append(errs, fmt.Errorf("%s: %d execution arms set (%s) — set exactly one of %s", where, len(set), strings.Join(set, ", "), strings.Join(execArms, "/")))
	}

	// co-fields.
	if v.forEach != "" && !v.steps {
		errs = append(errs, fmt.Errorf("%s: for_each requires a non-empty steps:", where))
	}
	if v.forEach == "" {
		var strays []string
		if v.steps {
			strays = append(strays, "steps")
		}
		if v.itemVar != "" {
			strays = append(strays, "item_var")
		}
		if v.indexVar != "" {
			strays = append(strays, "index_var")
		}
		if v.batchSize != 0 {
			strays = append(strays, "batch_size")
		}
		if v.pauseMs != 0 {
			strays = append(strays, "pause_ms")
		}
		if len(strays) > 0 {
			errs = append(errs, fmt.Errorf("%s: %s only apply alongside for_each", where, strings.Join(strays, "/")))
		}
	}
	if v.itemVar != "" && v.itemVar == v.indexVar {
		errs = append(errs, fmt.Errorf("%s: item_var and index_var must differ (both %q)", where, v.itemVar))
	}
	if v.parallelBranches == 0 && v.parallelWait != "" {
		errs = append(errs, fmt.Errorf("%s: parallel.wait is only meaningful with non-empty parallel branches", where))
	}
	if v.parallelWait != "" && !contains(waitModes, v.parallelWait) {
		errs = append(errs, fmt.Errorf("%s: parallel.wait %q is not one of all/any", where, v.parallelWait))
	}
	if v.input && v.approval {
		errs = append(errs, fmt.Errorf("%s: input and approval are mutually exclusive", where))
	}

	// a plan: step's own `$ref`s are the SAME earlier-step-only rule.
	for _, name := range v.planRefs {
		if !v.refsAmong[name] {
			errs = append(errs, fmt.Errorf("%s.plan: reference $%s.* names a step that is not declared earlier", where, name))
		}
	}

	// the lobster expression grammar + earlier-step-only references.
	if v.when != "" {
		if err := Validate(v.when); err != nil {
			errs = append(errs, fmt.Errorf("%s: when: %w", where, err))
		}
		errs = append(errs, checkEarlierRefs(where+".when", v.when, v.refsAmong)...)
	}
	errs = append(errs, checkEarlierRefs(where+".stdin", v.stdin, v.refsAmong)...)
	if v.forEach != "" {
		errs = append(errs, checkEarlierRefs(where+".for_each", v.forEach, v.refsAmong)...)
	}
	return errs
}

// checkEarlierRefs enforces that every step reference in s names a step declared
// EARLIER in the pipeline — lobster resolves a `$ref` from already-finished steps
// only, so a forward (or self) reference is a load-time error, not a runtime one.
func checkEarlierRefs(where, s string, defined map[string]bool) []error {
	var errs []error
	for _, name := range RefNames(s) {
		if !defined[name] {
			errs = append(errs, fmt.Errorf("%s: reference $%s.* names a step that is not declared earlier", where, name))
		}
	}
	return errs
}

// ValidateWorkflowCycles detects a cycle in a workflow→workflow reference graph
// (`name -> the workflow names its steps reach`). It is the graph primitive behind
// the "workflow: cycles" rule: a pipeline that reaches itself transitively can never
// terminate. Returns the cycle chain (`a -> b -> a`).
func ValidateWorkflowCycles(refs map[string][]string) error {
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var stack []string

	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case onStack:
			return fmt.Errorf("workflow cycle: %s", strings.Join(append(stack, name), " -> "))
		case done:
			return nil
		}
		state[name] = onStack
		stack = append(stack, name)
		for _, next := range refs[name] {
			if err := visit(next); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = done
		return nil
	}
	for name := range refs {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

// planRefNames returns the distinct `$step.path` references a plan's steps carry, in
// document order (the plan is marshalled and walked, so EVERY field is covered without
// enumerating the Op vocabulary here).
func planRefNames(plan []spec.Step) []string {
	if len(plan) == 0 {
		return nil
	}
	n, err := marshalNode(plan)
	if err != nil {
		return nil
	}
	return RefIDs(planRefs(n))
}

func nonZeroInput(i spec.PipelineInput) bool {
	return i.Prompt != "" || len(i.ResponseSchema) > 0 || len(i.Defaults) > 0
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
