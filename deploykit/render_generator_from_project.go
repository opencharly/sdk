package deploykit

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// render_generator_from_project.go — the SHARED construction source for a deploykit.Generator
// from a spec.ResolvedProject envelope (#67 / P11c). BOTH candy/plugin-build (the box-build
// render DRIVE, #67) AND candy/plugin-deploy-pod (the pod-overlay render, P11c) call this ONE
// helper — R3/DRY: ONE construction source, so the box build and the pod-overlay build construct
// their deploykit.Generator identically from the resolved-project envelope. plugin-build's former
// buildRenderGenerator was the sole caller; P11c extracts it here so plugin-deploy-pod reuses it
// for the overlay render.
//
// K3 render-seam production move, COMPLETED in K-wave 2 cone R1: the render now reaches the host
// for NOTHING. 5 of the original 9 host-coupled seams (RenderService, the detection/external builder
// resolves, ValidateEgress, RewriteHeaderCopy) went first — PURE registry resolve+Invoke dispatch
// (or pure data + host-fs I/O over the CandyModel envelope), RDD-spiked live on the external-builder
// leg. LocalPkg followed (W3): its "host rebuilds from the live *Candy graph" claim was stale — the
// caller already builds the step from dg.Candies/dg.Boxes. EmitPluginOp followed (P8b): its
// "only core can type-assert a BUILTIN provider's concrete type" claim was a concrete-type leak, not
// a seam. The LAST two, inline-builder + ensure-builders, fall here on the same finding: the inline
// resolve is the SAME OpResolve peer-dispatch resolveBuilderStage already runs for the detection and
// external legs, and the connect is ops.InvokeProviderOpts.ExtraRef (the host's generic
// connectPluginByWordRef Pass-2), which every plugin can request. So renderSeamCaller.hostBuild and
// the whole HostBuild("render-seam") kind are DELETED; only InvokeProvider peer-dispatch remains.

// renderSeamCaller holds the two dispatch primitives every wired seam needs (the venue executor
// + its context) so NewRenderGeneratorFromProject's own body stays a flat field-assignment list
// (R3 — the marshal/dispatch/decode boilerplate lives in ONE place, not repeated per seam).
type renderSeamCaller struct {
	ctx context.Context
	ex  *sdk.Executor
}

// invoke marshals params, InvokeProvider's (class, word, op) directly (peer-dispatch — no
// HostBuild round-trip), and decodes the reply into out. Used by the seams that are pure
// providerRegistry.resolve+Invoke dispatch — proven to need no host callback at all (K3 render-
// seam production move, RDD-spiked live on the external-builder leg).
func (c renderSeamCaller) invoke(class, word, op string, params, out any) error {
	pj, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("invoke %s:%s %s: marshal params: %w", class, word, op, err)
	}
	resJSON, err := c.ex.InvokeProvider(c.ctx, class, word, op, pj, nil, sdk.InvokeProviderOpts{})
	if err != nil {
		return fmt.Errorf("invoke %s:%s %s: %w", class, word, op, err)
	}
	if out != nil && len(resJSON) > 0 {
		if err := json.Unmarshal(resJSON, out); err != nil {
			return fmt.Errorf("invoke %s:%s %s: decode reply: %w", class, word, op, err)
		}
	}
	return nil
}

// validateEgress peer-dispatches to verb:egress's OpValidate (K3 — the render-seam's host
// egressValidate call was itself plain providerRegistry.resolve+Invoke, no host-only state).
func (c renderSeamCaller) validateEgress(kind, label, mode, data string) error {
	var reply struct {
		Error string `json:"error"`
	}
	if err := c.invoke("verb", "egress", sdk.OpValidate,
		map[string]string{"kind": kind, "label": label, "mode": mode, "data": data}, &reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s", reply.Error)
	}
	return nil
}

// resolveBuilderStage is the SHARED OpResolve Invoke+decode for the builder BUILDER leg (R3),
// direct peer-dispatch (K3, RDD-spiked live on the external leg): marshal the render context as
// params + a spec.BuildEnv descriptor as env, InvokeProvider the builder's OpResolve, decode the
// reply UNVALIDATED — the caller enforces the emptiness rule appropriate to its path. It serves ALL
// THREE builder legs: detection, external_builder:, and (since K-wave 2 cone R1) inline.
//
// ExtraRef carries the builder's canonical plugin-candy ref so the host's InvokeProvider falls back
// to connectPluginByWordRef's Pass-2 fetch for a build whose own candy closure vendors the plugin
// nowhere — a box/<distro> submodule that triggers a builder purely by DETECTION. This REPLACES the
// former ensure-builders host round-trip, which reached the identical two-pass
// ScanAllCandyWithConfigOpts + loadProjectPlugins machinery through a bespoke second copy (R3);
// connect is now on-demand per word at the moment of first use, and idempotent thereafter (the
// host resolves the registry before ever re-scanning).
func (c renderSeamCaller) resolveBuilderStage(word string, in spec.BuilderResolveInput, img *spec.ResolvedBox) (spec.BuilderResolveReply, error) {
	var reply spec.BuilderResolveReply
	env, err := json.Marshal(spec.BuildEnv{Distros: img.Tags, Image: img.Name})
	if err != nil {
		return reply, fmt.Errorf("marshal build env: %w", err)
	}
	params, err := json.Marshal(in)
	if err != nil {
		return reply, fmt.Errorf("marshal builder resolve input: %w", err)
	}
	opts := sdk.InvokeProviderOpts{}
	if ref, ok := spec.ExternalBuilderPluginRef(word); ok {
		opts.ExtraRef = ref
	}
	resJSON, ierr := c.ex.InvokeProvider(c.ctx, "builder", word, sdk.OpResolve, params, env, opts)
	if ierr != nil {
		return reply, ierr
	}
	if uerr := json.Unmarshal(resJSON, &reply); uerr != nil {
		return reply, fmt.Errorf("decode OpResolve reply: %w", uerr)
	}
	return reply, nil
}

// renderService materializes a ServiceEntry via candy/plugin-init's OpResolve + egress-gates it —
// direct peer-dispatch (K3). BuildServiceRenderContext is the SAME pure projection
// compile_service_steps.go's deploy-time renderServiceViaSeam uses — that call site is now a
// THIN wrapper delegating HERE (#55 W3 B4, R3: one shared implementation for both the build-time
// and deploy-time render-service paths, not two).
func (c renderSeamCaller) renderService(entry *spec.ServiceEntry, def *spec.ResolvedInit, ctx spec.ServiceRenderContext) (*spec.RenderedService, error) {
	if entry == nil {
		return nil, fmt.Errorf("RenderService: nil entry")
	}
	if def == nil || def.ServiceSchema == nil {
		return nil, fmt.Errorf("RenderService: init system has no service_schema")
	}
	ctx = spec.BuildServiceRenderContext(entry, ctx)
	var reply spec.ServiceRenderReply
	if err := c.invoke("kind", "init", sdk.OpResolve,
		spec.InitResolveRequest{Render: &spec.ServiceRenderInput{Init: def.Raw, Ctx: ctx}}, &reply); err != nil {
		return nil, err
	}
	rendered := reply.Rendered
	if rendered == nil {
		rendered = &spec.RenderedService{}
	}
	if rendered.UnitText != "" {
		if err := c.validateEgress("rendered_text", "service-unit:"+entry.Name, "text", rendered.UnitText); err != nil {
			return nil, err
		}
	}
	return rendered, nil
}

// NewRenderGeneratorFromProject constructs a deploykit.Generator from the resolved-project
// envelope + wires the host-coupled seams (the host callbacks the render still needs). It
// returns the Generator (the build order is the caller's responsibility — plugin-build computes
// it from the reply, plugin-deploy-pod computes the overlay candies from the live plans).
func NewRenderGeneratorFromProject(ctx context.Context, ex *sdk.Executor, rp *spec.ResolvedProject, dir string, devLocalPkg bool) (*Generator, error) {
	if rp == nil {
		return nil, fmt.Errorf("render: no resolved-project envelope")
	}

	dg := NewRenderGenerator()
	dg.Dir = dir
	dg.Tag = "" // tag is not needed for render (labels use EffectiveVersion)
	dg.BuildDir = filepath.Join(dir, ".build")
	dg.Containerfiles = make(map[string]string)
	dg.GlobalOrder = rp.GlobalOrder
	dg.RequestedBoxes = nil // the order is already filtered by the host
	dg.DevLocalPkg = devLocalPkg

	// Build the CandyModel map from the envelope.
	dg.Candies = make(map[string]CandyModel, len(rp.CandyModels))
	for name, cm := range rp.CandyModels {
		cv := rp.Candies[name]
		dg.Candies[name] = NewSpecCandyModel(cm, cv)
	}

	// Build the Boxes map from the envelope (re-attach build-render caches).
	dg.Boxes = make(map[string]*buildkit.ResolvedBox, len(rp.Boxes))
	for name, v := range rp.Boxes {
		dg.Boxes[name] = NewSpecResolvedBox(v, rp.Distro, rp.Builder)
	}

	// --- wire the seams ---
	c := renderSeamCaller{ctx: ctx, ex: ex}

	// EmitPluginOp: rendered FULLY plugin-side via a UNIFORM InvokeProvider(OpEmit) peer-dispatch
	// (P8b seam-death — the RenderSeamEmitPluginOp host callback is DELETED). A state-provision
	// verb self-declares its act shell via EmitReply.ActScript (the render RUN-wraps it via
	// EmitCmd), so no package-main concrete-type assert is needed: every verb — builtin or
	// external — dispatches through the SAME Invoke(OpEmit) envelope. The FULL op rides op.Params
	// (a state-provision act reads SHARED #Op modifiers — mode/content — beyond plugin_input);
	// the BuildEnv distros ride op.Env.
	dg.EmitPluginOp = func(op *spec.Op, img *spec.ResolvedBox) (string, bool, error) {
		params, err := json.Marshal(op)
		if err != nil {
			return "", false, fmt.Errorf("run: plugin verb %q build-emit: marshal op: %w", op.Plugin, err)
		}
		var distros []string
		if img != nil {
			distros = img.Tags
		}
		env, err := json.Marshal(spec.BuildEnv{Distros: distros})
		if err != nil {
			return "", false, fmt.Errorf("run: plugin verb %q build-emit: marshal build env: %w", op.Plugin, err)
		}
		resJSON, err := ex.InvokeProvider(ctx, "verb", op.Plugin, sdk.OpEmit, params, env, sdk.InvokeProviderOpts{})
		if err != nil {
			return "", false, fmt.Errorf("run: plugin verb %q build-emit: %w", op.Plugin, err)
		}
		var reply spec.EmitReply
		if len(resJSON) > 0 {
			if uerr := json.Unmarshal(resJSON, &reply); uerr != nil {
				return "", false, fmt.Errorf("run: plugin verb %q build-emit: decode OpEmit reply: %w", op.Plugin, uerr)
			}
		}
		if !reply.ActScript && strings.TrimSpace(reply.Fragment) == "" {
			return "", false, fmt.Errorf("run: plugin verb %q returned an empty OpEmit fragment — it has no build-context act (a runtime-only verb in a build run: step? use context: [runtime])", op.Plugin)
		}
		return reply.Fragment, reply.ActScript, nil
	}

	// CollectBoxPorts / CollectBoxVolume: from the envelope view (pre-computed by the host projector).
	dg.CollectBoxPorts = func(boxName string) ([]string, error) {
		if v, ok := rp.Boxes[boxName]; ok {
			return v.Ports, nil
		}
		return nil, nil
	}
	dg.CollectBoxVolume = func(boxName, home string) ([]VolumeMount, error) {
		v, ok := rp.Boxes[boxName]
		if !ok {
			return nil, nil
		}
		result := make([]VolumeMount, len(v.Volumes))
		for i := range v.Volumes {
			result[i] = VolumeMount{VolumeName: v.Volumes[i].VolumeName, ContainerPath: v.Volumes[i].ContainerPath}
		}
		return result, nil
	}

	// ValidateEgress / ValidateTextEgress: direct peer-dispatch (K3).
	dg.ValidateEgress = func(kind, label string, data []byte) error {
		return c.validateEgress(kind, label, "bytes", string(data))
	}
	dg.ValidateTextEgress = func(label, text string) error {
		return c.validateEgress("rendered_text", label, "text", text)
	}

	// RenderService: direct peer-dispatch (K3).
	dg.RenderService = c.renderService

	// ExternalizedBuilders: from the envelope (the registry D-FACT).
	dg.ExternalizedBuilders = rp.ExternalizedBuilders

	// RewriteHeaderCopyForRemote: pure data + host-fs I/O over dg.Candies (already exposes
	// Remote/SourceDir/SubPathPrefix/Name via the CandyModel interface, K3) — no host callback
	// needed at all.
	dg.RewriteHeaderCopyForRemote = func(headerCopy string) (string, error) {
		return rewriteHeaderCopyForRemote(dg.Candies, dir, dg.BuildDir, headerCopy)
	}

	// RenderLocalPkgImageInstall: direct call, NO host round-trip (W3 — the render-seam claim
	// of a genuine host dependency here was STALE). The caller (candy_steps.go) already builds
	// the step via CompileLocalPkgStep(layer, img, HostContext{}) using dg.Candies/dg.Boxes —
	// data ALREADY present in this envelope-hydrated Generator — so RenderLocalPkgImageInstall
	// needs nothing the host has that the plugin doesn't; it operates purely on the step it's
	// given (deploykit.RenderLocalPkgImageInstall, sdk/deploykit/localpkg.go).
	dg.RenderLocalPkgImageInstall = RenderLocalPkgImageInstall

	// ResolveInlineBuilder: direct peer-dispatch (K-wave 2 cone R1) — the LAST render seam to shed
	// its host round-trip. It is the SAME resolveBuilderStage the detection/external legs below use;
	// only the reply field it reads (InlineFragment, spliced in-candy) and its emptiness rule differ.
	// The former host seam's separate connect step is gone: resolveBuilderStage's ExtraRef carries
	// the canonical plugin-candy ref, so the host connects on demand during this very Invoke.
	dg.ResolveInlineBuilder = func(candyName, builderName string, bDef *buildkit.BuilderDef, ctx2 *spec.BuildStageContext, img *spec.ResolvedBox) (string, error) {
		// The resolve input carries the candy's INTRINSIC bare name, never the map key — they
		// diverge for a REMOTE candy (keyed by its fully-qualified ref). The deleted host seam read
		// it the same way (g.Candies[candyName].GetName()); the error messages keep using the key,
		// also as before.
		bare := candyName
		if layer := dg.Candies[candyName]; layer != nil {
			bare = layer.GetName()
		}
		in := spec.BuilderResolveInputFrom(bare, builderName, bDef, ctx2)
		reply, err := c.resolveBuilderStage(builderName, in, img)
		if err != nil {
			return "", fmt.Errorf("candy %q: inline builder %q resolve: %w", candyName, builderName, err)
		}
		if strings.TrimSpace(reply.InlineFragment) == "" {
			return "", fmt.Errorf("candy %q: inline builder %q returned an empty OpResolve inline fragment", candyName, builderName)
		}
		return reply.InlineFragment, nil
	}

	// ResolveDetectionBuilderStage / ResolveExternalBuilderStage: direct peer-dispatch (K3,
	// RDD-spiked live on the external leg).
	dg.ResolveDetectionBuilderStage = c.resolveBuilderStage
	dg.ResolveExternalBuilderStage = func(word, candyName string, img *spec.ResolvedBox) (spec.BuilderResolveReply, error) {
		reply, err := c.resolveBuilderStage(word, spec.BuilderResolveInput{Candy: candyName}, img)
		if err != nil {
			return spec.BuilderResolveReply{}, err
		}
		if strings.TrimSpace(reply.Stage) == "" {
			return spec.BuilderResolveReply{}, fmt.Errorf("external builder %q returned an empty OpResolve stage — it has no build-context builder", word)
		}
		return reply, nil
	}

	// EmitBakedPlugins: direct call (K3 build-tail move, coneB-buildtail) — buildPluginBinary is
	// 100% pure os/exec (proven by the already-moved ensureCharlyBinaryFresh), so this needs no
	// host round-trip at all; the former "bake-plugins" HostBuild seam (charly/host_build_bake_plugins.go)
	// is DELETED.
	dg.EmitBakedPlugins = func(b *strings.Builder, boxName string, candyOrder []string) error {
		return EmitBakedPlugins(ctx, b, dg.BuildDir, boxName, candyOrder, dg.Candies)
	}

	return dg, nil
}
