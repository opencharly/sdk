package deploykit

import (
	"github.com/opencharly/sdk/spec"
)

// description_collect.go — collect the baked `plan:` view for the ai.opencharly.description
// OCI label (FLOOR-SLIM Unit 4, moved from charly/description_collect.go). Its real consumers
// today (render_baked_metadata.go's OCI-label bake, resolved_project_host.go's
// "resolved-project" HostBuild seam, check_cmd.go's CLI-free live-check gather engine) are ALL
// still core-resident, K1/K3-blocked files that already import deploykit freely — so
// deploykit.CollectDescriptions is a normal same-pattern kit-consumption call needing zero new
// seam, unlike a plugin destination which would force those 3 still-core callers to reach it
// over InvokeProvider/HostBuild for no functional reason (their own K1/K3 unmigrated status is
// the actual blocker, not this file's shape). BoxCandyChain (config_candy_chain.go) already
// lives here (fs-bundle's FLOOR-SLIM Unit-1 landing); OpInContext (compiler_deps.go) is the
// EXISTING DI-hook charly injects at init (deploykit.OpInContext = opInContext, layers.go) — this
// file calls that package var directly, no new injection needed. stampStepIntentDo is 100% pure
// over spec.Step (spec.StepDoMode), so it moves here unconditionally, no seam at all.
//
// CollectDescriptions walks the base-box chain for boxName and gathers every kind: entity's
// plan: into a three-section LabelDescriptionSet. The walk mirrors CollectHooks and
// CollectShell (both still core, package-main-coupled): candy-order per level, then step into
// internal base, dedupe by candy name, stop at first external base OR on cycle.
//
// Bake rule (what goes IN the label): the VERIFICATION + runtime-provisioning view of plan: —
// every check:/agent-check: step plus any run: step whose context: includes runtime
// (plan-runtime provisioning a checker needs). Pure build/deploy-context run: steps (the
// install timeline) are NOT baked — they are consumed by the InstallPlan→Containerfile/
// DeployExecutor and are already materialized in the image. agent-run:/include: steps are not
// baked (agent-run appears only in deploy-level iterate: plans; include is expanded into
// iterate plans, never a candy bake).
//
// MergeDeployDescriptions (the per-host deploy-plan overlay onto a baked LabelDescriptionSet)
// lives in sdk/kit (description_merge.go) — pure over LabelDescriptionSet/spec.Step, zero core
// state. Its callers (check_cmd.go's checkLivePod, the "live" gather engine) stay core (they
// need LoadUnified/ExtractMetadata) and call kit.MergeDeployDescriptions.

// stampStepIntentDo writes the keyword-derived do-mode onto a verb-carrying step's Op.IntentDo
// (via the shared spec.StepDoMode derivation), so the baked ai.opencharly.description label
// carries intent_do deterministically. Pure over spec.Step — verb-less agent-check steps keep
// an empty IntentDo.
func stampStepIntentDo(s *spec.Step) {
	if len(s.VerbsSet()) == 0 {
		return
	}
	s.IntentDo = string(spec.StepDoMode(s))
}

// BakeableSteps returns the subset of a plan that belongs in the runtime descriptor label per
// the bake rule above.
func BakeableSteps(plan []spec.Step) []spec.Step {
	var out []spec.Step
	for _, s := range plan {
		bake := false
		switch {
		case s.Check != "" || s.AgentCheck != "":
			bake = true
		case s.Run != "" && OpInContext(&s.Op, spec.CtxRuntime):
			bake = true
		}
		if !bake {
			continue
		}
		// DELIBERATE collect-time stamp: write the keyword-derived do-mode onto the baked COPY so
		// the ai.opencharly.description label carries intent_do. This was formerly a side effect of
		// the in-core validate mutating the shared structs the bake serialized; when the validate
		// ENGINE moved to candy/plugin-box (K3-D+) it began stamping only its envelope copy, so the
		// bake must now stamp its own output.
		stampStepIntentDo(&s)
		out = append(out, s)
	}
	return out
}

// CollectDescriptions returns nil if every section is empty.
func CollectDescriptions(cfg *spec.Config, layers map[string]spec.CandyReader, boxName string) *spec.LabelDescriptionSet {
	set := &spec.LabelDescriptionSet{}

	allCandyNames, _ := BoxCandyChain(cfg, layers, boxName)
	for _, candyName := range allCandyNames {
		layer, ok := layers[candyName]
		if !ok {
			continue
		}
		baked := BakeableSteps(layer.PlanSteps())
		if layer.GetDescription() == "" && len(baked) == 0 {
			continue
		}
		set.Candy = append(set.Candy, spec.LabeledDescription{
			Origin:      "candy:" + candyName,
			Description: layer.GetDescription(),
			Plan:        baked,
		})
	}

	// Box-level description + plan.
	if img, ok := cfg.BoxConfig(boxName); ok {
		baked := BakeableSteps(img.Plan)
		if img.Description != "" || len(baked) > 0 {
			set.Box = append(set.Box, spec.LabeledDescription{
				Origin:      "box:" + boxName,
				Description: img.Description,
				Plan:        baked,
			})
		}
	}

	if set.IsEmpty() {
		return nil
	}
	return set
}
