// Package checkkit — the SHARED check-drive pieces every check-driving plugin
// (plugin-check, plugin-pipeline, ...) consumes: the plan grammar, the
// out-of-process verb resolver, and the ADE agent-check grader. One home (R3):
// the pieces were plugin-local in plugin-check; they belong in the SDK so any
// plugin drives the org's check machinery identically.
package checkkit

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// PlanGrammar implements kit.PlanGrammar — the do-mode/context grammar, ported
// from the core hostPlanGrammar (pure over spec.Op + the VerbCatalog).
type PlanGrammar struct{}

func (PlanGrammar) EffectiveDo(op *spec.Op) spec.DoMode {
	switch spec.DoMode(op.IntentDo) {
	case spec.DoAct, spec.DoAssert, spec.DoInstruct:
		return spec.DoMode(op.IntentDo)
	}
	verb, err := op.Kind()
	if err == nil {
		if vs, ok := spec.VerbCatalog[verb]; ok && vs.DefaultDo != "" {
			return vs.DefaultDo
		}
	}
	return spec.DoAssert
}

func (PlanGrammar) InContext(op *spec.Op, runtime bool) bool {
	wantCtx := spec.CtxBuild
	if runtime {
		wantCtx = spec.CtxRuntime
	}
	return slices.Contains(effectiveContexts(op), wantCtx)
}

func (PlanGrammar) ContextsLabel(op *spec.Op) string {
	return fmt.Sprintf("%v", effectiveContexts(op))
}

func effectiveContexts(op *spec.Op) []spec.ExecContext {
	if len(op.Context) > 0 {
		out := make([]spec.ExecContext, 0, len(op.Context))
		for _, s := range op.Context {
			out = append(out, spec.ExecContext(s))
		}
		return out
	}
	if verb, err := op.Kind(); err == nil {
		if vs, ok := spec.VerbCatalog[verb]; ok {
			return vs.Contexts
		}
	}
	return nil
}

// VerbResolver implements kit.VerbResolver — the out-of-process verb dispatch
// over the plugin's single dial (InvokeProvider), with the venue descriptor
// threaded from the runner's current executor.
type VerbResolver struct {
	Ex  *sdk.Executor
	Env spec.CheckEnv
	kr  *kit.Runner
}

func (r *VerbResolver) SetRunner(kr *kit.Runner) { r.kr = kr }

func (r *VerbResolver) RunVerb(ctx context.Context, op *spec.Op) (spec.CheckResult, bool) {
	if r.Ex == nil {
		return spec.CheckResult{Status: spec.StatusFail, Message: "checkkit: no host executor (the dial is unavailable)"}, true
	}

	word, err := op.Kind()
	if err != nil {
		return spec.CheckResult{}, false
	}
	params, err := json.Marshal(op)
	if err != nil {
		return spec.CheckResult{Status: spec.StatusFail, Message: "verb " + word + ": marshal op: " + err.Error()}, true
	}
	env := r.Env
	if r.kr != nil {
		env = SnapshotCheckEnv(r.kr)
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return spec.CheckResult{Status: spec.StatusFail, Message: "verb " + word + ": marshal env: " + err.Error()}, true
	}
	opts := sdk.InvokeProviderOpts{}
	if r.kr != nil {
		if de, ok := r.kr.Exec().(spec.DeployExecutor); ok {
			if d := kit.DescriptorFromExecutor(de); d.Kind != "" {
				opts.VenueDescriptor = &d
			}
		}
	}
	resultJSON, err := r.Ex.InvokeProvider(ctx, "verb", word, sdk.OpRun, params, envJSON, opts)
	if err != nil {
		return spec.CheckResult{Status: spec.StatusFail, Message: "verb " + word + ": " + err.Error()}, true
	}
	var res spec.CheckResult
	if len(resultJSON) > 0 {
		if uerr := json.Unmarshal(resultJSON, &res); uerr != nil {
			return spec.CheckResult{Status: spec.StatusFail, Message: "verb " + word + ": decode result: " + uerr.Error()}, true
		}
	}
	return res, true
}

// SnapshotCheckEnv builds the wire CheckEnv snapshot from the runner's state.
func SnapshotCheckEnv(kr *kit.Runner) spec.CheckEnv {
	if kr == nil {
		return spec.CheckEnv{Mode: "live"}
	}

	return spec.CheckEnv{
		Mode:      "live",
		Box:       kr.Box(),
		Instance:  kr.Instance(),
		Distros:   kr.Distros(),
		VenueKind: venueKindOf(kr),
	}
}

func venueKindOf(kr *kit.Runner) string {
	if de, ok := kr.Exec().(spec.DeployExecutor); ok {
		if d := kit.DescriptorFromExecutor(de); d.Kind != "" {
			return d.Kind
		}
	}
	return ""
}

// AdeGrader grades an agent-check step's prose against the venue evidence via
// the caller's agent runtime (the ADE contract: the live agent decides pass/fail).
type AdeGrader func(ctx context.Context, prose string) (bool, string)
