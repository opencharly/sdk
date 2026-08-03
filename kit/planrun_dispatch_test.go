package kit

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// planrun_dispatch_test.go — additional RunOne/RunPlan semantics coverage relocated from
// charly/*_test.go (the #55 decoupling cone, Batch D): these tests assert kit.RunOne/
// kit.RunPlan's OWN wrapper behavior (variable expansion, empty-verb/no-verb handling,
// unresolved-var fail-vs-skip, SkipDeterministicRun, agent-run not swept as a deterministic
// install, and per-step Origin re-stamping) — zero charly dispatch coupling once driven
// through the existing fakePlanContext/fakeVerbResolver fixtures (planrun_test.go, R3: no
// new fixture shape introduced).

// TestRunOne_EmptyOpFails: a zero-value Op (no verb keyword at all) fails fast at
// op.Kind()'s error, before any dispatch — relocated from charly's TestRunner_EmptyCheck.
func TestRunOne_EmptyOpFails(t *testing.T) {
	pc := &fakePlanContext{env: map[string]string{}, verbs: &fakeVerbResolver{}}
	r := RunOne(context.Background(), pc, &spec.Op{})
	if r.Status != StatusFail || !strings.Contains(r.Message, "no verb") {
		t.Fatalf("empty op → %v %q, want StatusFail mentioning 'no verb'", r.Status, r.Message)
	}
}

// TestRunOne_VariableExpansion: ${VAR} in plugin_input is expanded from EffectiveEnv BEFORE
// dispatch; an unresolved reference reports SKIP with the unresolved key named. Relocated
// from charly's TestRunner_VariableExpansion.
func TestRunOne_VariableExpansion(t *testing.T) {
	t.Run("expanded", func(t *testing.T) {
		vr := &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}
		pc := &fakePlanContext{env: map[string]string{"HOST_PORT:6379": "16379"}, verbs: vr}
		op := &spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "redis-cli -p ${HOST_PORT:6379}"}}
		r := RunOne(context.Background(), pc, op)
		if r.Status != StatusPass {
			t.Fatalf("expected pass, got %+v", r)
		}
		if vr.lastOp == nil || vr.lastOp.PluginInput["command"] != "redis-cli -p 16379" {
			t.Fatalf("var not expanded before dispatch: %+v", vr.lastOp)
		}
	})

	t.Run("unresolved → skip", func(t *testing.T) {
		vr := &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}
		pc := &fakePlanContext{env: map[string]string{}, verbs: vr}
		op := &spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "redis-cli -p ${HOST_PORT:6379}"}}
		r := RunOne(context.Background(), pc, op)
		if r.Status != StatusSkip || !strings.Contains(r.Message, "unresolved") {
			t.Fatalf("expected skip with unresolved, got %+v", r)
		}
	})
}

// TestRunOne_UnresolvedHostVarFails: an unresolvable ${HOST:…} (cross-member address)
// FAILS the check (a SKIP there would be a fake pass on an unreachable dependency), while a
// non-host unresolved var stays a legitimate SKIP. Relocated from charly's
// check_members_test.go TestRunOne_UnresolvedHostVarFails (the command itself is never
// executed — the var-resolution gate returns before any dispatch).
func TestRunOne_UnresolvedHostVarFails(t *testing.T) {
	vr := &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}
	pc := &fakePlanContext{env: map[string]string{}, verbs: vr}

	hostOp := &spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "curl -fsS http://${HOST:absent:80}/"}}
	if r := RunOne(context.Background(), pc, hostOp); r.Status != StatusFail {
		t.Errorf("unresolved ${HOST:…} → status %v (%q), want StatusFail", r.Status, r.Message)
	}
	otherOp := &spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "echo ${SOME_UNSET_VAR}"}}
	if r := RunOne(context.Background(), pc, otherOp); r.Status != StatusSkip {
		t.Errorf("unresolved non-host var → status %v (%q), want StatusSkip", r.Status, r.Message)
	}
}

// TestRunPlan_SkipDeterministicRunSkipsInstall: SkipDeterministicRun (the `charly check
// feature run` ADE-acceptance mode) skips DETERMINISTIC run: install steps, still runs
// check: steps, and does NOT sweep up agent-run: as a deterministic install (it reaches the
// agent/no-grader path instead). Relocated from charly's plan_unify_test.go
// TestPlanUnify_SkipDeterministicRunSkipsInstall — regression for #16 (feature-run re-ran
// run: steps against a live pod where /ctx no longer exists).
func TestRunPlan_SkipDeterministicRunSkipsInstall(t *testing.T) {
	vr := &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}
	pc := &fakePlanContext{env: map[string]string{}, skipRun: true, verbs: vr}
	set := &LabelDescriptionSet{Candy: []LabeledDescription{{
		Origin: "candy:x",
		Plan: []spec.Step{
			{Run: "pip install /ctx/pkg", Op: spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "false"}}}, // would FAIL if executed
			{Check: "the marker resolves", Op: spec.Op{Plugin: "matching", PluginInput: map[string]any{"matching": "m", "contains": map[string]any{"contains": "m"}}}},
			{AgentRun: "an agent drives the UI", Op: spec.Op{}}, // agent step, NOT a deterministic install
		},
	}}}
	res := RunPlan(context.Background(), pc, set, false)
	if len(res) != 3 {
		t.Fatalf("want 3 step results, got %d", len(res))
	}
	if res[0].Keyword != string(KwRun) || res[0].Result.Status != StatusSkip {
		t.Errorf("run: install step should be skipped under SkipDeterministicRun, got keyword=%q status=%v", res[0].Keyword, res[0].Result.Status)
	}
	if !strings.Contains(res[0].Result.Message, "install-timeline") {
		t.Errorf("skip reason should name the install-timeline, got %q", res[0].Result.Message)
	}
	if res[1].Keyword != string(KwCheck) || res[1].Result.Status != StatusPass {
		t.Errorf("check: step should run, got keyword=%q status=%v", res[1].Keyword, res[1].Result.Status)
	}
	if strings.Contains(res[2].Result.Message, "install-timeline") {
		t.Errorf("agent-run: must NOT be skipped as a deterministic install step, got %q", res[2].Result.Message)
	}
}

// TestRunPlan_StampsStepOrigin: the per-step Origin is re-stamped from the owning
// LabeledDescription (op.Origin = fs.origin) — the candy-group Origin lives ONCE on the
// LabeledDescription and is NOT baked per-step, so RunPlan must propagate it onto each
// dispatched Op or a host-side consumer keyed on Origin (the committed-APK anchor,
// charly/checkrun_charly_verbs.go's resolveCheckApk) sees an empty Origin. Relocated from
// charly's apk_format_test.go TestRunPlan_StampsStepOrigin, simplified to assert the
// stamping directly via the resolver's captured Op rather than round-tripping through a
// stub external adb provider + a scan-error sentinel message.
func TestRunPlan_StampsStepOrigin(t *testing.T) {
	vr := &fakeVerbResolver{known: true, result: spec.CheckResult{Status: StatusPass}}
	pc := &fakePlanContext{env: map[string]string{}, verbs: vr}
	const wantOrigin = "candy:github.com/owner/repo/candy/android-emulator-layer"
	set := &LabelDescriptionSet{Candy: []LabeledDescription{{
		Origin:      wantOrigin,
		Description: "android apps install",
		Plan: []spec.Step{{
			Op: spec.Op{ID: "adb-install-apidemos", Plugin: "adb", PluginInput: map[string]any{"method": "install", "apk": "./tests/data/ApiDemos-debug.apk"}, Context: []string{"runtime"}},
		}},
	}}}
	res := RunPlan(context.Background(), pc, set, false)
	if len(res) != 1 {
		t.Fatalf("want 1 step result, got %d", len(res))
	}
	if vr.lastOp == nil || vr.lastOp.Origin != wantOrigin {
		t.Fatalf("step Origin was not stamped onto the dispatched Op: got %+v, want Origin %q", vr.lastOp, wantOrigin)
	}
}
