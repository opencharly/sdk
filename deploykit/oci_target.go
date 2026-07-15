package deploykit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/spec"
)

// oci_target.go — OCITarget, the POD-OVERLAY deploy-mode Containerfile renderer (P11c
// relocation from charly/build_target_oci.go). It is the deploy-mode sibling of the box-build
// render (render_generator.go): where the box build emits one Containerfile per image by
// walking each candy's `ops` (tasks_render.go / tasks_emit.go), the pod-overlay build emits
// ONE overlay Containerfile for a set of add_candy candies by walking each candy's
// InstallPlan steps. The compiler-emitted step kinds' BUILD-emit is served by the compiled-in
// class:step plugin candy/plugin-installstep; the HOST-COUPLED kinds (system-packages/builder/
// local-pkg-install/op) call back the host build engine over the step-emit seam — exactly as
// the in-core OCITarget did before the move.
//
// WHY this lives in sdk/deploykit (not charly core): the per-kind step DATA layer
// (spec.InstallStep + the 13 concrete structs + ExternalStep/IsExternalStepKind/StepToView) is
// already sdk-importable (sdk/spec + sdk/deploykit), so the kind-blind WALKER (this file) is a
// shared kind-blind render M-mechanism importable by BOTH candy/plugin-build (box) and
// candy/plugin-deploy-pod (overlay) — R3, no duplication. The kind-specific step DISPATCH
// (the providerRegistry.resolve(ClassStep, word) + the in-proc reverse-channel OpEmit +
// stepProviderFor.EmitOCI for ExternalPlugin) stays CORE (the host-side overlay builder
// machinery, a kind-blind M-mechanism dispatched by word against the provider registry) and is
// reached through the EmitStepOp seam the candy wires via HostBuild("render-seam","oci-emit-step").
// The host seam reuses the EXACT former in-core emitStep/spliceClassStepEmit/stepProviderFor
// funcs, so the rendered fragment is byte-identical to the pre-move core render (byte-parity by
// construction, mirroring #67's render-seam contract). See P11c-VERDICT.md for the full design.
//
// The walker carries ONLY the scalars it reads itself — Dir (the host cache key for the seam),
// Home (for ResolveHome), Distros (the per-step distros threaded onto the class:step OpEmit) —
// NOT a *buildkit.ResolvedBox (the candy cannot construct one without the full Distro/Builder
// maps, and the host-coupled render execution stays host-side behind the seam anyway). The host
// overlay-prep returns Home/Distros to the candy (via a render-seam method), so the candy-side
// walker never holds the heavy host state.

// OCITarget emits Containerfile directives for an InstallPlan. One instance handles one
// overlay build; the caller (candy/plugin-deploy-pod's podPrepareVenue) constructs a target,
// calls Emit with the add_candy plans, and reads the rendered fragment via String.
type OCITarget struct {
	// Dir is the project dir — the key the EmitStepOp seam (HostBuild("render-seam",
	// "oci-emit-step")) uses to look up the host-side cached overlay buildEngineContext (the
	// host overlay-prep constructs + caches it per dir, mirroring renderGenCache for the box
	// build). The live *Generator / DistroDef / BuilderConfig / Box / ImageBuildDir /
	// ContextRelPrefix the host step-emitters need never cross the wire — only this dir key does.
	Dir string

	// Home is the overlay base image's runtime home — read by the walker for ResolveHome (the
	// `{{.Home}}` token in home-bearing step fields resolves to this, the home the baked paths
	// run under). The host overlay-prep returns it to the candy (via a render-seam method); the
	// walker never holds the full ResolvedBox.
	Home string

	// Distros is the overlay base image's distro tag set (Box.Tags) — threaded onto each step's
	// EmitStepOp seam call so the host class:step OpEmit can gate a plugin's build-emit on the
	// image's distro set (the SAME datum the former in-core OCITarget passed to spliceClassStepEmit).
	Distros []string

	// EmitStepOp is the host-coupled step-render seam: it renders ONE InstallStep's
	// Containerfile fragment via the CORE provider-registry dispatch (spliceClassStepEmit for
	// the 12 compiler-emitted plugin-served kinds + the authored external step; stepProviderFor
	// .EmitOCI for ExternalPlugin), using the host-side overlay Generator + buildEngineContext
	// cached by overlay-prep. The candy wires this via HostBuild("render-seam","oci-emit-step")
	// (out-of-process) or an in-proc closure (compiled-in / tests). A step that emits nothing
	// (a deploy-only step, or VenueSkip) returns "". Nil EmitStepOp => a no-op render (the walker
	// still writes the `# Layer:` header + the home resolution; tests that exercise the dispatch
	// wire a real seam or stay in core).
	EmitStepOp func(step spec.InstallStep, plan *spec.InstallPlan, distros []string) (string, error)

	buf strings.Builder
}

// Name identifies this target.
func (t *OCITarget) Name() string { return "oci" }

// Emit walks each plan's steps and appends Containerfile directives to the internal buffer.
// Multiple plans emit sequentially (per-candy), mirroring the pre-move in-core OCITarget.Emit.
func (t *OCITarget) Emit(plans []*spec.InstallPlan, _ spec.EmitOpts) error {
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		if err := t.emitPlan(plan); err != nil {
			return fmt.Errorf("OCITarget.Emit(%s): %w", plan.Candy, err)
		}
	}
	return nil
}

// String returns the accumulated Containerfile fragment.
func (t *OCITarget) String() string { return t.buf.String() }

// emitPlan emits directives for one candy's plan. It resolves the deferred {{.Home}} token in
// home-bearing step fields to the image's runtime home, writes the `# Layer:` header, then
// walks each step. The skip-on-image-build behaviour (VenueSkip) + the per-kind dispatch live
// behind the EmitStepOp seam (the host); a step whose seam returns "" contributes nothing (a
// deploy-only step).
func (t *OCITarget) emitPlan(plan *spec.InstallPlan) error {
	if t.Home != "" {
		ResolveHome(plan, t.Home)
	}
	fmt.Fprintf(&t.buf, "# Layer: %s\n", plan.Candy)
	for _, step := range plan.Steps {
		if step == nil {
			continue
		}
		if step.Venue() == spec.VenueSkip {
			continue
		}
		frag, err := t.emitStep(step, plan)
		if err != nil {
			return err
		}
		if frag == "" {
			continue
		}
		t.buf.WriteString(frag)
		if !strings.HasSuffix(frag, "\n") {
			t.buf.WriteString("\n")
		}
	}
	t.buf.WriteString("\n")
	return nil
}

// emitStep dispatches one step to the host seam (the core provider-registry dispatch). The
// former in-core type-switch is gone: the 12 compiler-emitted plugin-served kinds + the
// authored external step route through spliceClassStepEmit, and ExternalPlugin through its
// StepProvider.EmitOCI — BOTH behind the EmitStepOp seam. A nil seam (tests / a no-op render)
// returns "" so the walker produces only the layer header + home resolution.
func (t *OCITarget) emitStep(step spec.InstallStep, plan *spec.InstallPlan) (string, error) {
	if t.EmitStepOp == nil {
		return "", nil
	}
	return t.EmitStepOp(step, plan, t.Distros)
}

// OCIEmitStepParams is the plain-Go param struct the candy marshals into HostBuild("step-emit",
// StepEmitRequest{Word:"oci-emit-step", Payload: <OCIEmitStepParams>}).Distros}) — the per-step
// render seam for the pod-overlay build (P11c). It rides INSIDE the opaque StepEmitRequest.Payload
// bytes (a deploykit-internal dispatch detail, NOT a boundary-validated wire contract — the CUE
// wire type is StepEmitRequest itself; this is the SAME convention as the render-seam param structs
// in render_seam.go). Dir is the project dir — the key the host "oci-emit-step" emitter uses to look
// up the cached overlay buildEngineContext (the live *Generator/DistroDef/BuilderConfig/Box cannot
// cross the wire). StepView is the step's wire form (the candy serializes via StepToView); PlanView
// is the step's plan wire form (WireView) — the host reconstructs both via StepFromView/PlanFromView
// + calls ociEmitStep (the full provider-registry dispatch: 12 compiler-emitted kinds + ExternalPlugin
// + authored external), returning the rendered Containerfile fragment.
type OCIEmitStepParams struct {
	Dir      string               `json:"dir,omitempty"`
	StepView spec.InstallStepView `json:"step_view,omitempty"`
	PlanView spec.InstallPlanView `json:"plan_view,omitempty"`
}
