package loaderkit

// validate_check_beds.go — the LOAD-time kind:check bed invariants (K1-LOADER RELOCATION, moved from
// charly/unified.go). Registry-free: it reads the registry-derived spec.Threaded DATA snapshot
// (DeployTraits for bed-target classification, DeploySubstrates for external-substrate recognition)
// instead of querying the live provider registry, so it runs identically host-side OR plugin-side
// (boundary law clause D). Behaviour byte-identical to the former charly validateCheckBeds /
// ValidateIterateBed.

import (
	"fmt"
	"strings"

	"github.com/opencharly/spec/spec"
)

// ValidateCheckBeds enforces the kind:check bed-specific invariants beyond the generic deploy
// validation (which already runs on the folded beds via ValidateDeploymentTree, covering the pod
// `box:` requirement). Runs at LOAD time so EVERY command that resolves a bed (charly check run,
// charly fleet add, charly config, charly box validate, …) sees the same friendly error.
func ValidateCheckBeds(uf *spec.UnifiedFile, t spec.Threaded) error {
	for name, node := range uf.CheckBeds() {
		// An iterate: bed is a benchmark (the former kind:score), NOT a deterministic R10 bed: it
		// drives the AI loop scoring its plan's check:/agent-check: steps against an
		// operator-provisioned sandbox, so the target/disposable/cross-ref requirements do not apply.
		// Validate the iterate block instead.
		if node.Iterate != nil {
			if err := ValidateIterateBed(uf, name, &node); err != nil {
				return err
			}
			continue
		}
		// Disposable is the sole authorization for the destroy+rebuild the R10 sequence drives; a
		// non-disposable bed can't be rebuilt unattended (see /charly-internals:disposable).
		if !node.IsDisposable() {
			return fmt.Errorf(
				"kind:check bed %q must set `disposable: true` — `charly check run` destroys + rebuilds it unattended (R10 acceptance gate)",
				name)
		}
		// Bed-target validity is DATA-DRIVEN from the substrate's declared #DeployTraits
		// (candy/plugin-substrate), never a per-substrate-word switch (the boundary-law incomplete-seam
		// gate, task #22): bed_target marks pod/vm/local/android as valid bed targets; kubernetes
		// (bed_target:false) and unknown words fall to the external/unsupported arm. image_backed
		// distinguishes pod's box: cross-ref (enforced elsewhere) from the template-backed
		// vm/local/android from: cross-ref.
		traits := t.DeployTraits[node.Target]
		switch {
		case node.Target == "":
			// A GROUP bed (no workload cross-ref) — valid ONLY when it carries sibling Members
			// (subject + driver peers): the §3 group+siblings shape for cross-deployment probing.
			if len(node.Members) == 0 {
				return fmt.Errorf("kind:check bed %q has no workload cross-ref and no sibling members — a group bed must declare member subdeployments (the subject + driver of a cross-deployment probe)", name)
			}
		case traits != nil && traits.BedTarget:
			// A valid bed target. image_backed (pod) enforces box: via validateDeployRequiresBox on the
			// folded Deploy entry — no duplicate check. The template-backed substrates (vm/local/android)
			// share ONE cross-ref shape: a `from: <entity>` naming an entry in the SAME PluginKinds[target]
			// map every standalone-template kind folds into.
			if !traits.ImageBacked {
				if node.From == "" {
					return fmt.Errorf("kind:check bed %q (target: %s) must set `%s: <entity>`", name, node.Target, node.Target)
				}
				if _, ok := uf.PluginKinds[node.Target][node.From]; !ok {
					// The from: name:tag DEPLOY-HOP (Phase 3): the from: may name a kind:check BED
					// (the clone-base bed) whose own from: names the template.
					if _, isBed := uf.CheckBeds()[node.From]; !isBed {
						return fmt.Errorf("kind:check bed %q references %s entity %q which is not defined", name, node.Target, node.From)
					}
				}
			}
		default:
			// An external (out-of-process) deploy substrate (e.g. `exampledeploy`): the provider applies
			// the deployment via the E3b reverse channel; it composes its candies via add_candy: and
			// carries no from:/image: cross-ref to validate here. Recognized via the EXACT host
			// isExternalDeploySubstrate DATA snapshot (Threaded.ExternalDeploySubstrates, filled by the
			// host's own predicate) — NOT a reconstruction, and NOT a core in-process substrate (kubernetes has
			// traits but bed_target:false, stays unsupported as a bed target).
			if t.ExternalDeploySubstrates[node.Target] {
				break
			}
			return fmt.Errorf("kind:check bed %q has unsupported target %q (must be pod, vm, local, android, or a registered external deploy substrate)", name, node.Target)
		}
	}
	return nil
}

// ValidateIterateBed enforces the iterate: benchmark invariants (replaces the former
// validateScoreNode/validateHarnessSemantics). An iterate bed is exempt from the deterministic R10 bed
// rules (target/disposable/cross-ref); instead: every iterate.agent[] references an entry in the
// `agent:` catalog; iterate.sandbox names a deployment (non-empty); and the bed's plan: carries at
// least one direct `check:` step. Pure — reads uf.PluginKinds["agent"] + node.Iterate + node.Plan.
func ValidateIterateBed(uf *spec.UnifiedFile, name string, node *spec.FleetNode) error {
	it := node.Iterate
	agents := uf.PluginKinds["agent"] // agent is a plugin kind; opaque name-keyed catalog
	for _, a := range it.Agent {
		if _, ok := agents[a]; !ok {
			return fmt.Errorf("iterate bed %q: agent %q is not defined in the agent: catalog", name, a)
		}
	}
	if strings.TrimSpace(it.Sandbox) == "" {
		return fmt.Errorf("iterate bed %q: iterate.sandbox must name a deployment (pod|vm|host) where the agent + charly run", name)
	}
	checks := 0
	for i := range node.Plan {
		if node.Plan[i].Check != "" {
			checks++
		}
	}
	if checks == 0 {
		return fmt.Errorf("iterate bed %q: plan must contain at least one `check:` step (the scored success criteria)", name)
	}
	return nil
}
