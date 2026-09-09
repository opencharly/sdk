package checkkit

import (
	"context"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestPlanGrammarEffectiveDo(t *testing.T) {
	g := PlanGrammar{}
	op := &spec.Op{IntentDo: string(spec.DoAct)}
	if g.EffectiveDo(op) != spec.DoAct {
		t.Fatal("explicit intent should win")
	}
	op2 := &spec.Op{}
	if g.EffectiveDo(op2) != spec.DoAssert {
		t.Fatal("default should be DoAssert")
	}
}

func TestPlanGrammarInContext(t *testing.T) {
	g := PlanGrammar{}
	op := &spec.Op{Context: []string{string(spec.CtxRuntime)}}
	if !g.InContext(op, true) {
		t.Fatal("runtime context should be in-context")
	}
	if g.InContext(op, false) {
		t.Fatal("runtime op should not be in the build context")
	}
}

func TestSnapshotCheckEnvNilRunner(t *testing.T) {
	env := SnapshotCheckEnv(nil)
	if env.Mode != "live" {
		t.Fatalf("mode = %q, want live", env.Mode)
	}
}

func TestVerbResolverNilExecutor(t *testing.T) {
	r := &VerbResolver{}
	_, ok := r.RunVerb(context.Background(), &spec.Op{Plugin: "check"})
	if !ok {
		t.Fatal("a nil executor must still return a handled result")
	}
}

func TestRunProvisionAct_NilExecutorMapsToFail(t *testing.T) {
	r := &VerbResolver{}
	res, ok := r.RunProvisionAct(context.Background(), &spec.Op{Plugin: "check"}, "check")
	if !ok {
		t.Fatal("a nil executor must still return a handled result")
	}
	if res.Status != spec.StatusFail {
		t.Errorf("status = %v, want fail (the nil-executor guard)", res.Status)
	}
}
