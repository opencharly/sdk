package deploykit

// compile_construct_step.go — K5-A item 1 (compile-seam ctx-threading): replaces the
// core-init()-set deploykit.CompileActOp func var with an explicit ctx/exec-threaded
// dispatcher. The GENERIC (non-`plugin:`-verb) op lowering — the vast majority of a
// candy's install timeline (mkdir/copy/write/link/download/setcap/build/command) — is
// FULLY PORTABLE (no core-only dependency at all) and now lives here directly, in
// buildGenericOpStep, moved verbatim from the tail of the former charly/
// install_build_act.go:compileActOp. ONLY a `run: plugin: <word>` op needs the host's
// PROVIDER REGISTRY (a clause-M kernel mechanism) to decide how it lowers — that ONE
// sub-case reaches back via the "construct-step" HostBuild seam
// (spec.ConstructStepRequest/Reply, sdk/schema/seam.cue), so the wire round-trip is paid
// ONLY for the rare plugin-verb case, never for the common install-verb case.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// constructStepSeamKind names the HostBuild seam charly/host_build_construct_step.go
// serves.
const constructStepSeamKind = "construct-step"

// constructOpStep lowers a single install-timeline op into the right InstallStep.
// Non-`plugin:` verbs (the common case) never touch the wire: buildGenericOpStep is a
// pure function of op/layer/img. A `plugin:` verb reaches the "construct-step" seam,
// which runs the SAME registry-consult logic charly/install_build_act.go's
// compileActOp used to run in-proc — a nil reply.Step means "not special, build the
// generic OpStep" (the host already ran that decision; the caller does the
// construction to avoid a redundant round-trip content-wise, not to re-decide).
func constructOpStep(ctx context.Context, ex *sdk.Executor, op *spec.Op, layer CandyModel, img *ResolvedBox) (InstallStep, error) {
	verb, err := op.Kind()
	if err != nil {
		return nil, nil
	}
	if verb != "plugin" {
		return buildGenericOpStep(op, layer, img), nil
	}
	if ex == nil {
		return nil, fmt.Errorf("construct-step: no host reverse channel (command not compiled-in?)")
	}
	userDir, _ := ResolveUserSpec(op.RunAs, img)
	req := spec.ConstructStepRequest{
		Op:             *op,
		CandyName:      layer.GetName(),
		CandySourceDir: layer.GetSourceDir(),
		ResolvedUser:   userDir,
		PkgFormat:      img.Pkg,
		DistroTags:     append([]string(nil), img.Tags...),
	}
	if len(layer.Vars()) > 0 {
		req.CandyVars = make(map[string]string, len(layer.Vars()))
		maps.Copy(req.CandyVars, layer.Vars())
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("construct-step: marshal request: %w", err)
	}
	resJSON, err := ex.HostBuild(ctx, constructStepSeamKind, reqJSON)
	if err != nil {
		return nil, fmt.Errorf("construct-step: %w", err)
	}
	var reply spec.ConstructStepReply
	if len(resJSON) > 0 {
		if err := json.Unmarshal(resJSON, &reply); err != nil {
			return nil, fmt.Errorf("construct-step: decode reply: %w", err)
		}
	}
	if reply.Step == nil {
		return buildGenericOpStep(op, layer, img), nil
	}
	step, err := StepFromView(*reply.Step)
	if err != nil {
		return nil, fmt.Errorf("construct-step: decode step view: %w", err)
	}
	return step, nil
}

// buildGenericOpStep lowers an install verb / command / non-typed plugin verb into the
// generic OpStep (existing emit + Reverse). Moved verbatim from the tail of the former
// charly/install_build_act.go:compileActOp — fully portable, no core-only dependency.
// Snapshot layer.Vars() so the host/local renderer can emit `export K=V` (build-time
// gets these via Containerfile ENV). Tokenize a home-relative `to:` so each DeployTarget
// resolves it against the real destination home at emit.
func buildGenericOpStep(op *spec.Op, layer CandyModel, img *ResolvedBox) InstallStep {
	userDir, _ := ResolveUserSpec(op.RunAs, img)
	var candyVars map[string]string
	if len(layer.Vars()) > 0 {
		candyVars = make(map[string]string, len(layer.Vars()))
		maps.Copy(candyVars, layer.Vars())
	}
	var resolvedTo string
	if op.To != "" {
		resolvedTo = kit.ExpandPath(op.To, HomeToken)
	}
	return &OpStep{
		Op:           op,
		CandyName:    layer.GetName(),
		CandyDir:     layer.GetSourceDir(),
		CtxPath:      layer.GetSourceDir(),
		ResolvedUser: userDir,
		CandyVars:    candyVars,
		To:           resolvedTo,
		Distros:      img.Tags,
	}
}
