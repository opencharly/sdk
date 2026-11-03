package kit

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// stepenv_test.go — the DATA channel's coverage.
//
// Every test here fails without the change: the context implements the optional
// StepEnvExporter seam, and the walk must carry the host's values into the op the verb
// receives — a value that is DATA must arrive INTACT (the shell never re-parses the
// result of an expansion), and a name the shell cannot export must be DISCLOSED rather
// than dropped in silence (a value that vanishes silently reads exactly like a host that
// never supplied it).

// stepEnvContext is a PlanContext that ALSO implements the optional StepEnvExporter seam —
// the shape a host that wants a step to SEE values (not interpolate them) has.
type stepEnvContext struct {
	fakePlanContext
	stepEnv map[string]string
}

func (c *stepEnvContext) StepEnv() map[string]string { return c.stepEnv }

// stepEnvVerb is a context + verb that record the op the verb received.
func stepEnvVerb(t *testing.T, exported map[string]string) (*stepEnvContext, *fakeVerbResolver) {
	t.Helper()
	vr := &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}
	pc := &stepEnvContext{stepEnv: exported}
	pc.verbs = vr
	return pc, vr
}

// TestRunOne_StepEnvReachesTheStepAsData: a value carrying shell metacharacters reaches
// the step's environment INTACT. The text channel would have made it SOURCE (the shell
// re-parses a substituted value); the environment is DATA, so the value arrives as one
// word, unexpanded and unquoted.
func TestRunOne_StepEnvReachesTheStepAsData(t *testing.T) {
	const evil = "$(touch /tmp/marker) a b;c * ${HOME}"
	pc, vr := stepEnvVerb(t, map[string]string{"EVIL": evil})
	if r := RunOne(context.Background(), pc, pluginOp()); r.Status != StatusPass {
		t.Fatalf("run → %v %q, want StatusPass", r.Status, r.Message)
	}
	if vr.lastOp == nil {
		t.Fatal("the verb received no op")
	}
	got, ok := vr.lastOp.Env["EVIL"]
	if !ok {
		t.Fatalf("the exported value did not reach the step's env: %v", vr.lastOp.Env)
	}
	if got != evil {
		t.Fatalf("the exported value was rewritten: got %q, want %q", got, evil)
	}
}

// TestRunOne_StepEnvReachesALowercaseName: the expander's grammar is upper-case-initial, so
// a lowercase name is unreachable through the TEXT channel — the value vanished. The data
// channel carries ANY name, because a name is a name, not a pattern.
func TestRunOne_StepEnvReachesALowercaseName(t *testing.T) {
	pc, vr := stepEnvVerb(t, map[string]string{"lower": "v"})
	RunOne(context.Background(), pc, pluginOp())
	if got := vr.lastOp.Env["lower"]; got != "v" {
		t.Fatalf("the lowercase name did not reach the step's env: %v", vr.lastOp.Env)
	}
}

// TestRunOne_StepEnvDoesNotMutateThePlanOp: the walk copies the Op STRUCT but SHARES its
// Env map, so merging in place would leak one run's values into the next run of the same
// plan. The plan's own op must come out untouched.
func TestRunOne_StepEnvDoesNotMutateThePlanOp(t *testing.T) {
	pc, _ := stepEnvVerb(t, map[string]string{"X": "1"})
	op := pluginOp()
	op.Env = map[string]string{"Y": "2"}
	RunOne(context.Background(), pc, op)
	if _, ok := op.Env["X"]; ok {
		t.Fatalf("the plan's own op was mutated in place: %v", op.Env)
	}
	if len(op.Env) != 1 {
		t.Fatalf("the plan's own env changed shape: %v", op.Env)
	}
}

// TestRunOne_StepEnvDoesNotOverrideTheStepOwnEnv: the step's own `env:` is the author
// speaking last about their own step; the host's value is a default.
func TestRunOne_StepEnvDoesNotOverrideTheStepOwnEnv(t *testing.T) {
	pc, vr := stepEnvVerb(t, map[string]string{"X": "host"})
	op := pluginOp()
	op.Env = map[string]string{"X": "step"}
	RunOne(context.Background(), pc, op)
	if got := vr.lastOp.Env["X"]; got != "step" {
		t.Fatalf("the host's value overrode the step's own env: got %q, want %q", got, "step")
	}
}

// TestRunOne_StepEnvRejectsANonIdentifierNameAndSaysSo: `export HOST_PORT:6379='v'` is a
// shell SYNTAX ERROR that would fail the whole step, so the name is not exported — and it
// is DISCLOSED, because a value that vanishes silently reads exactly like a host that
// never supplied it.
func TestRunOne_StepEnvRejectsANonIdentifierNameAndSaysSo(t *testing.T) {
	pc, vr := stepEnvVerb(t, map[string]string{"HOST_PORT:6379": "16379"})
	r := RunOne(context.Background(), pc, pluginOp())
	if _, ok := vr.lastOp.Env["HOST_PORT:6379"]; ok {
		t.Fatalf("a name the shell cannot export was exported: %v", vr.lastOp.Env)
	}
	if !strings.Contains(r.Message, "HOST_PORT:6379") {
		t.Fatalf("the rejected name was not disclosed: %q", r.Message)
	}
	if r.Status != StatusPass {
		t.Fatalf("the rejection changed the verdict: %v %q", r.Status, r.Message)
	}
}

// TestRunOne_NoStepEnvExporterExportsNothing: a host that does not implement the optional
// seam keeps exactly the behaviour it had before the channel existed.
func TestRunOne_NoStepEnvExporterExportsNothing(t *testing.T) {
	pc := &fakePlanContext{env: map[string]string{}, verbs: &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}}
	op := pluginOp()
	op.Env = map[string]string{"Y": "2"}
	RunOne(context.Background(), pc, op)
	if len(op.Env) != 1 {
		t.Fatalf("a host without the seam got a changed env: %v", op.Env)
	}
}

// TestMergeStepEnv: the merge's own contract.
func TestMergeStepEnv(t *testing.T) {
	t.Run("no export leaves the map alone", func(t *testing.T) {
		in := map[string]string{"A": "1"}
		got, rejected := MergeStepEnv(in, nil)
		if len(got) != 1 || got["A"] != "1" {
			t.Fatalf("merge = %v, want the step's own map", got)
		}
		if len(rejected) != 0 {
			t.Fatalf("rejected = %v, want none", rejected)
		}
	})
	t.Run("the step's own env wins", func(t *testing.T) {
		got, _ := MergeStepEnv(map[string]string{"A": "step"}, map[string]string{"A": "host", "B": "host"})
		if got["A"] != "step" {
			t.Fatalf("A = %q, want the step's own %q", got["A"], "step")
		}
		if got["B"] != "host" {
			t.Fatalf("B = %q, want the host's %q", got["B"], "host")
		}
	})
	t.Run("the map is copied, not mutated", func(t *testing.T) {
		in := map[string]string{"A": "1"}
		got, _ := MergeStepEnv(in, map[string]string{"B": "2"})
		got["A"] = "mutated"
		if in["A"] != "1" {
			t.Fatalf("the caller's map was mutated: %v", in)
		}
	})
	t.Run("a name the shell cannot export is rejected, and named", func(t *testing.T) {
		got, rejected := MergeStepEnv(nil, map[string]string{"HOST_PORT:6379": "1", "ok_name": "2", "1bad": "3"})
		if _, ok := got["HOST_PORT:6379"]; ok {
			t.Fatalf("a rejected name was exported: %v", got)
		}
		if got["ok_name"] != "2" {
			t.Fatalf("a valid name was not exported: %v", got)
		}
		if len(rejected) != 2 || rejected[0] != "1bad" || rejected[1] != "HOST_PORT:6379" {
			t.Fatalf("rejected = %v, want the two invalid names, sorted", rejected)
		}
	})
}
