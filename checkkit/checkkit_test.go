package checkkit

import (
	"context"
	"testing"

	"github.com/opencharly/sdk/kit"
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

// TestSnapshotCheckEnvCarriesCandyDirs is the regression guard for the committed-APK
// fixture anchor on the plugin→host wire leg: SnapshotCheckEnv MUST carry the runner's
// CandyDirs, or the host's resolveCheckApk sees an empty map and every relative
// `apk:` step fails with `candy "…" is absent from the source scan (0 candies scanned)`.
// (The host→plugin snapshot already carries CandyDirs; only this leg was missing it —
// a regression from the R3 checkkit one-home cutover, which replaced the plugin-local
// snapshot that DID set it.)
func TestSnapshotCheckEnvCarriesCandyDirs(t *testing.T) {
	dirs := map[string]string{"github.com/opencharly/pod-android-emulator-layer": "/cache/pod-android-emulator-layer"}
	kr := kit.NewRunner(kit.RunnerConfig{CandyDirs: dirs})

	ce := SnapshotCheckEnv(kr)
	if len(ce.CandyDirs) != len(dirs) {
		t.Fatalf("SnapshotCheckEnv CandyDirs = %v, want %v", ce.CandyDirs, dirs)
	}
	if ce.CandyDirs["github.com/opencharly/pod-android-emulator-layer"] != dirs["github.com/opencharly/pod-android-emulator-layer"] {
		t.Errorf("SnapshotCheckEnv dropped/mangled CandyDirs: got %v, want %v", ce.CandyDirs, dirs)
	}
}
