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
// charly deploy add, charly config, charly box validate, …) sees the same friendly error.
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
			// A GROUP bed (no workload cross-ref) — valid ONLY when it carries members
			// (subject + driver peers): the §3 group+siblings shape for cross-deployment
			// probing. The ONE ordered member tree counts both positions.
			//
			// DECLARED-but-UNCONNECTED structural-kind exemption (the recognizedKind fallback):
			// after the group:-kind removal a TARGETLESS bed node can only be an external
			// STRUCTURAL plugin kind (every resource deploy kind carries #DeployTraits). A
			// structural kind DECLARED in the schema vocabulary (t.StructuralKinds — connected
			// providers ∪ parse-time pre-scan declarations) whose serving provider did NOT
			// connect (no registered input schema — the documented no-declared-schema signal,
			// the same absence the parse's in-body member scan falls back on) cannot be
			// member-scanned: its OpLoad never ran, so the bed folds without its authored
			// member tree. Membership semantics come from the PARENT kind's declared schema,
			// not from runtime registry state — the same declared-fields contract the parse's
			// StructuralDeclaredFields channel implements — so the member requirement must not
			// fire on a fold the plugin never had the chance to shape (a fresh clone or a
			// degraded environment hits exactly this on a structural-kind witness bed). A
			// CONNECTED structural kind (schema registered — its OpLoad dispatch hard-requires
			// it) whose bed is STILL targetless+memberless is a real defect (the plugin was
			// there and did not fold the authored members) and keeps failing.
			if !node.HasMembers() && !declaredStructuralKindUnconnected(t) {
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
		// Cutover C task 4 — ALL-OR-NOTHING stage authoring per bed: when ANY step
		// of the bed (the root node AND every member, both positions) carries the
		// `stage:` shared modifier, EVERY step must. A mixed bed (some staged, some
		// not) would make the stage-grouped walk ambiguous (task 5: stages dominate;
		// an unstaged step has no group). Stage names are dot-free — enforced by the
		// CUE regex on #Op (`=~"^[^.]+$"`) — and the FIRST-SEEN stage order is
		// recorded at load by the walk from the authored order (kit.RunPlanStaged).
		if verr := validateBedStages(name, &node); verr != nil {
			return verr
		}
	}
	return nil
}

// validateBedStages enforces the ALL-OR-NOTHING stage-authoring invariant for ONE
// kind:check bed (Cutover C task 4): EVERY plan step of the bed root and its
// WHOLE uniform member tree (deploy-level AND in-substrate, recursively) must
// either all carry `stage:` or none may. "Per bed" = the full tree — a member
// with a mixed set would make the stage-grouped walk ambiguous (task 5: stages
// dominate; an unstaged step has no group). Stage names are dot-free — enforced
// by the CUE regex on #Op (`=~"^[^.]+$"`) — and the FIRST-SEEN stage order is
// recorded at load by the walk from the authored order (kit.RunPlanStaged).
// Pure.
func validateBedStages(name string, node *spec.DeployNode) error {
	var steps []spec.Step
	var collect func(n *spec.DeployNode)
	collect = func(n *spec.DeployNode) {
		if n == nil {
			return
		}
		steps = append(steps, n.Plan...)
		for i := range n.Member {
			if n.Member[i].Node != nil {
				collect(n.Member[i].Node)
			}
		}
	}
	collect(node)

	staged := 0
	for i := range steps {
		if steps[i].Stage != "" {
			staged++
		}
	}
	if len(steps) > 0 && staged > 0 && staged != len(steps) {
		return fmt.Errorf(
			"kind:check bed %q: mixed stage authoring — %d of %d plan steps across the bed and its members carry stage:, but %d do not; stage authoring is all-or-nothing per bed (every step of a bed must carry stage: when any step does)",
			name, staged, len(steps), len(steps)-staged)
	}
	return nil
}

// declaredStructuralKindUnconnected reports whether the Threaded snapshot declares a STRUCTURAL
// plugin kind whose serving provider did not connect: the word is in the recognized structural
// vocabulary (t.StructuralKinds — the recognizedKind fallback the validator consults instead of
// the live registry, clause D) but has NO registered input schema (absent from
// t.StructuralDeclaredFields — the host leaves a word whose schema is not loaded absent, the
// documented no-declared-schema fallback; a connected structural kind's OpLoad dispatch
// hard-requires the registered def, so presence proves connection). The folded DeployNode does not
// carry its discriminator word (a structural kind's OpLoad reply is a plain spec.Deploy), so the
// exemption is deliberately load-scoped and conservative: it only fires when at least one
// declared structural kind is unconnected, and never when every declared structural kind
// connected (a complete StructuralDeclaredFields map exempts nothing).
func declaredStructuralKindUnconnected(t spec.Threaded) bool {
	for word := range t.StructuralKinds {
		if _, connected := t.StructuralDeclaredFields[word]; !connected {
			return true
		}
	}
	return false
}

// ValidateIterateBed enforces the iterate: benchmark invariants (replaces the former
// validateScoreNode/validateHarnessSemantics). An iterate bed is exempt from the deterministic R10 bed
// rules (target/disposable/cross-ref); instead: every iterate.agent[] references an entry in the
// `agent:` catalog; iterate.sandbox names a deployment (non-empty); and the bed's plan: carries at
// least one direct `check:` step. Pure — reads uf.PluginKinds["agent"] + node.Iterate + node.Plan.
func ValidateIterateBed(uf *spec.UnifiedFile, name string, node *spec.DeployNode) error {
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
