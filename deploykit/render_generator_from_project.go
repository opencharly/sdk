package deploykit

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/spec"
)

// render_generator_from_project.go — the SHARED construction source for a deploykit.Generator
// from a spec.ResolvedProject envelope (#67 / P11c). BOTH candy/plugin-build (the box-build
// render DRIVE, #67) AND candy/plugin-deploy-pod (the pod-overlay render, P11c) call this ONE
// helper — R3/DRY: ONE construction source, so the box build and the pod-overlay build construct
// their deploykit.Generator identically from the resolved-project envelope. plugin-build's former
// buildRenderGenerator was the sole caller; P11c extracts it here so plugin-deploy-pod reuses it
// for the overlay render.
//
// K3 render-seam production move: 5 of the original 9 host-coupled seams (RenderService, the
// detection/external builder resolves, ValidateEgress, RewriteHeaderCopy) were PURE
// providerRegistry.resolve+Invoke dispatch (or pure data + host-fs I/O over the CandyModel
// envelope) — RDD-spiked live on the external-builder leg, proven to need no host callback at
// all — so they now dispatch directly via renderSeamCaller.invoke (InvokeProvider, no
// HostBuild round-trip). The 4 REMAINING seams (EmitPluginOp, localpkg, inline-builder,
// ensure-builders) + EmitBakedPlugins have a genuine host-only dependency (the live loader's
// scan+connect machinery, or a Go-level type-assertion against a BUILTIN provider's concrete
// type) and still call back via HostBuild("render-seam"/"bake-plugins").

// renderSeamCaller holds the two dispatch primitives every wired seam needs (the venue executor
// + its context) so NewRenderGeneratorFromProject's own body stays a flat field-assignment list
// (R3 — the marshal/dispatch/decode boilerplate lives in ONE place, not repeated per seam).
type renderSeamCaller struct {
	ctx context.Context
	ex  *sdk.Executor
}

// hostBuild dispatches one host-coupled render seam to the host via HostBuild("render-seam")
// (#67). params is the per-method deploykit param struct; out is the per-method result struct to
// decode into (nil for void methods). The host calls the corresponding CORE function
// (byte-parity by construction) and returns its error string verbatim in reply.Error.
func (c renderSeamCaller) hostBuild(method string, params, out any) error {
	pj, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("render-seam %s: marshal params: %w", method, err)
	}
	reqJSON, err := json.Marshal(spec.RenderSeamRequest{Method: method, Params: pj})
	if err != nil {
		return fmt.Errorf("render-seam %s: marshal request: %w", method, err)
	}
	replyJSON, err := c.ex.HostBuild(c.ctx, "render-seam", reqJSON)
	if err != nil {
		return fmt.Errorf("render-seam %s: %w", method, err)
	}
	var reply spec.RenderSeamReply
	if err := json.Unmarshal(replyJSON, &reply); err != nil {
		return fmt.Errorf("render-seam %s: decode reply: %w", method, err)
	}
	if reply.Error != "" {
		// The host's exact core-function error string — re-emitted byte-identical.
		return fmt.Errorf("%s", reply.Error)
	}
	if out != nil && len(reply.Result) > 0 {
		if err := json.Unmarshal(reply.Result, out); err != nil {
			return fmt.Errorf("render-seam %s: decode result: %w", method, err)
		}
	}
	return nil
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
	resJSON, err := c.ex.InvokeProvider(c.ctx, class, word, op, pj, nil)
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
// reply UNVALIDATED — the caller enforces the emptiness rule appropriate to its path.
func (c renderSeamCaller) resolveBuilderStage(word string, in spec.BuilderResolveInput, img *buildkit.ResolvedBox) (spec.BuilderResolveReply, error) {
	var reply spec.BuilderResolveReply
	env, err := json.Marshal(spec.BuildEnv{Distros: img.Tags, Image: img.Name})
	if err != nil {
		return reply, fmt.Errorf("marshal build env: %w", err)
	}
	params, err := json.Marshal(in)
	if err != nil {
		return reply, fmt.Errorf("marshal builder resolve input: %w", err)
	}
	resJSON, ierr := c.ex.InvokeProvider(c.ctx, "builder", word, sdk.OpResolve, params, env)
	if ierr != nil {
		return reply, ierr
	}
	if uerr := json.Unmarshal(resJSON, &reply); uerr != nil {
		return reply, fmt.Errorf("decode OpResolve reply: %w", uerr)
	}
	return reply, nil
}

// renderService materializes a ServiceEntry via candy/plugin-init's OpResolve + egress-gates it —
// direct peer-dispatch (K3). BuildServiceRenderContext is the SAME pure projection charly's own
// (deploy-mode) RenderService calls (R3, one shared source).
func (c renderSeamCaller) renderService(entry *spec.ServiceEntry, def *spec.ResolvedInit, ctx spec.ServiceRenderContext) (*spec.RenderedService, error) {
	if entry == nil {
		return nil, fmt.Errorf("RenderService: nil entry")
	}
	if def == nil || def.ServiceSchema == nil {
		return nil, fmt.Errorf("RenderService: init system has no service_schema")
	}
	ctx = BuildServiceRenderContext(entry, ctx)
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

	// EmitPluginOp: still host-side — the ProvisionActor/BuildEmitter type-assertion only charly
	// core can perform against a BUILTIN provider's concrete type (its generic Invoke() is an
	// intentional error-stub for placement-invisible perf, so InvokeProvider can't substitute).
	dg.EmitPluginOp = func(op *spec.Op, img *buildkit.ResolvedBox) (string, bool, error) {
		var res EmitPluginOpResult
		if err := c.hostBuild(RenderSeamEmitPluginOp, EmitPluginOpParams{Dir: dir, BoxName: img.Name, Op: op}, &res); err != nil {
			return "", false, err
		}
		return res.Out, res.IsScript, nil
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

	// RenderLocalPkgImageInstall: still host-side — the host rebuilds the LocalPkgInstallStep
	// from the live *Candy graph (the SAME CompileLocalPkgStep origin/main used) + renders it.
	dg.RenderLocalPkgImageInstall = func(step *LocalPkgInstallStep, devLocalPkg bool, imageDir, boxName string) (string, error) {
		var res LocalPkgResult
		if err := c.hostBuild(RenderSeamLocalPkg, LocalPkgParams{Dir: dir, BoxName: boxName, CandyName: step.CandyName, ImageDir: imageDir, DevLocalPkg: devLocalPkg}, &res); err != nil {
			return "", err
		}
		return res.Fragment, nil
	}

	// ResolveInlineBuilder: still host-side — rides K1 with EnsureBuilders (its embedded connect
	// is the same loader scan+connect action, usually a no-op but not guaranteed).
	dg.ResolveInlineBuilder = func(candyName, builderName string, bDef *buildkit.BuilderDef, ctx2 *spec.BuildStageContext, img *buildkit.ResolvedBox) (string, error) {
		var res InlineBuilderResult
		if err := c.hostBuild(RenderSeamInlineBuilder, InlineBuilderParams{Dir: dir, BoxName: img.Name, CandyName: candyName, BuilderName: builderName, BDef: bDef, Ctx: ctx2}, &res); err != nil {
			return "", err
		}
		return res.Fragment, nil
	}

	// EnsureBuildersConnected: still host-side — a genuine loader action (ScanAllCandyWithConfigOpts
	// + loadProjectPlugins), not a data lookup. Rides K1 (#40).
	dg.EnsureBuildersConnected = func(detected []string) error {
		return c.hostBuild(RenderSeamEnsureBuilders, EnsureBuildersParams{Dir: dir, Words: detected}, nil)
	}

	// ResolveDetectionBuilderStage / ResolveExternalBuilderStage: direct peer-dispatch (K3,
	// RDD-spiked live on the external leg).
	dg.ResolveDetectionBuilderStage = c.resolveBuilderStage
	dg.ResolveExternalBuilderStage = func(word, candyName string, img *buildkit.ResolvedBox) (spec.BuilderResolveReply, error) {
		reply, err := c.resolveBuilderStage(word, spec.BuilderResolveInput{Candy: candyName}, img)
		if err != nil {
			return spec.BuilderResolveReply{}, err
		}
		if strings.TrimSpace(reply.Stage) == "" {
			return spec.BuilderResolveReply{}, fmt.Errorf("external builder %q returned an empty OpResolve stage — it has no build-context builder", word)
		}
		return reply, nil
	}

	// EmitBakedPlugins: via HostBuild("bake-plugins") — the host builds + stages plugin binaries +
	// returns the COPY/chmod fragment.
	dg.EmitBakedPlugins = func(b *strings.Builder, boxName string, candyOrder []string) error {
		reqJSON, err := json.Marshal(spec.BakePluginsRequest{Dir: dir, BoxName: boxName, CandyOrder: candyOrder})
		if err != nil {
			return fmt.Errorf("bake-plugins: marshal request: %w", err)
		}
		replyJSON, err := ex.HostBuild(ctx, "bake-plugins", reqJSON)
		if err != nil {
			return fmt.Errorf("bake-plugins: %w", err)
		}
		var reply spec.BakePluginsReply
		if err := json.Unmarshal(replyJSON, &reply); err != nil {
			return fmt.Errorf("bake-plugins: decode reply: %w", err)
		}
		if reply.Error != "" {
			return fmt.Errorf("bake-plugins: %s", reply.Error)
		}
		b.WriteString(reply.Fragment)
		return nil
	}

	return dg, nil
}
