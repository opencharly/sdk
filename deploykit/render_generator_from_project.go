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
// their deploykit.Generator identically from the resolved-project envelope. The host-coupled
// seams (the 9 render-seam methods + EmitBakedPlugins) call back to the host via HostBuild /
// the in-proc reverse channel (placement-invisible: compiled-in goes in-proc, out-of-process
// goes over gRPC). plugin-build's former buildRenderGenerator was the sole caller; P11c extracts
// it here so plugin-deploy-pod reuses it for the overlay render.
//
// The 9 render-seam host-coupled cases are K3 build-engine migration INVENTORY (tracked, named
// exit — a later slice dissolves them by moving the render execution fully into the candy); they
// are NOT new violations introduced by P11c.

// NewRenderGeneratorFromProject constructs a deploykit.Generator from the resolved-project
// envelope + wires the host-coupled seams (the host callbacks the render needs). It returns the
// Generator (the build order is the caller's responsibility — plugin-build computes it from the
// reply, plugin-deploy-pod computes the overlay candies from the live plans). The seams that call
// back to the host use HostBuild / InvokeProvider over the in-proc reverse channel
// (placement-invisible: compiled-in goes in-proc, out-of-process goes over gRPC).
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

	// --- wire the host-coupled seams ---

	// renderSeam dispatches one host-coupled render seam to the host via HostBuild("render-seam")
	// (#67). params is the per-method deploykit param struct; out is the per-method result struct
	// to decode into (nil for void methods). The host calls the corresponding CORE function
	// (byte-parity by construction) and returns its error string verbatim in reply.Error.
	renderSeam := func(method string, params any, out any) error {
		pj, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("render-seam %s: marshal params: %w", method, err)
		}
		reqJSON, err := json.Marshal(spec.RenderSeamRequest{Method: method, Params: pj})
		if err != nil {
			return fmt.Errorf("render-seam %s: marshal request: %w", method, err)
		}
		replyJSON, err := ex.HostBuild(ctx, "render-seam", reqJSON)
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

	// EmitPluginOp: dispatch a plugin verb through the host Provider registry (the host
	// checks ProvisionActor act-shell vs OpEmit fragment — logic that stays host-side).
	dg.EmitPluginOp = func(op *spec.Op, img *buildkit.ResolvedBox) (string, bool, error) {
		var res EmitPluginOpResult
		if err := renderSeam(RenderSeamEmitPluginOp, EmitPluginOpParams{Dir: dir, BoxName: img.Name, Op: op}, &res); err != nil {
			return "", false, err
		}
		return res.Out, res.IsScript, nil
	}

	// CollectBoxPorts: from the envelope view (pre-computed by the host projector).
	dg.CollectBoxPorts = func(boxName string) ([]string, error) {
		if v, ok := rp.Boxes[boxName]; ok {
			return v.Ports, nil
		}
		return nil, nil
	}

	// CollectBoxVolume: from the envelope view (pre-computed by the host projector).
	dg.CollectBoxVolume = func(boxName, home string) ([]VolumeMount, error) {
		if v, ok := rp.Boxes[boxName]; ok {
			result := make([]VolumeMount, len(v.Volumes))
			for i := range v.Volumes {
				result[i] = VolumeMount{
					VolumeName:    v.Volumes[i].VolumeName,
					ContainerPath: v.Volumes[i].ContainerPath,
				}
			}
			return result, nil
		}
		return nil, nil
	}

	// ValidateEgress: the host runs the egress CUE validation (bytes mode — traefik routes, …).
	dg.ValidateEgress = func(kind, label string, data []byte) error {
		return renderSeam(RenderSeamValidateEgress, ValidateEgressParams{Kind: kind, Mode: "bytes", Label: label, Data: data}, nil)
	}

	// ValidateTextEgress: the host gates the rendered Containerfile text (rendered_text/text).
	dg.ValidateTextEgress = func(label, text string) error {
		return renderSeam(RenderSeamValidateEgress, ValidateEgressParams{Kind: "rendered_text", Mode: "text", Label: label, Data: []byte(text)}, nil)
	}

	// RenderService: the host materializes a ServiceEntry via candy/plugin-init (OpResolve) +
	// egress-gates it (the core RenderService func, byte-exact).
	dg.RenderService = func(entry *spec.ServiceEntry, def *spec.ResolvedInit, ctx spec.ServiceRenderContext) (*spec.RenderedService, error) {
		var res RenderServiceResult
		if err := renderSeam(RenderSeamRenderService, RenderServiceParams{Dir: dir, Entry: entry, Def: def, Ctx: ctx}, &res); err != nil {
			return nil, err
		}
		return res.Rendered, nil
	}

	// ExternalizedBuilders: from the envelope (the registry D-FACT).
	dg.ExternalizedBuilders = rp.ExternalizedBuilders

	// RewriteHeaderCopyForRemote: host-fs materialization (the core gen.rewriteHeaderCopyForRemote).
	dg.RewriteHeaderCopyForRemote = func(headerCopy string) (string, error) {
		var res RewriteHeaderCopyResult
		if err := renderSeam(RenderSeamRewriteHeaderCopy, RewriteHeaderCopyParams{Dir: dir, HeaderCopy: headerCopy}, &res); err != nil {
			return "", err
		}
		return res.HeaderCopy, nil
	}

	// RenderLocalPkgImageInstall: the host rebuilds the LocalPkgInstallStep from the live
	// *Candy graph (the SAME CompileLocalPkgStep origin/main used) + renders it (byte-exact).
	dg.RenderLocalPkgImageInstall = func(step *LocalPkgInstallStep, devLocalPkg bool, imageDir, boxName string) (string, error) {
		var res LocalPkgResult
		if err := renderSeam(RenderSeamLocalPkg, LocalPkgParams{Dir: dir, BoxName: boxName, CandyName: step.CandyName, ImageDir: imageDir, DevLocalPkg: devLocalPkg}, &res); err != nil {
			return "", err
		}
		return res.Fragment, nil
	}

	// ResolveInlineBuilder: the host connects + OpResolves an externalized INLINE builder
	// (the core gen.resolveInlineBuilderSeam, byte-exact).
	dg.ResolveInlineBuilder = func(candyName, builderName string, bDef *buildkit.BuilderDef, ctx2 *spec.BuildStageContext, img *buildkit.ResolvedBox) (string, error) {
		var res InlineBuilderResult
		if err := renderSeam(RenderSeamInlineBuilder, InlineBuilderParams{Dir: dir, BoxName: img.Name, CandyName: candyName, BuilderName: builderName, BDef: bDef, Ctx: ctx2}, &res); err != nil {
			return "", err
		}
		return res.Fragment, nil
	}

	// EnsureBuildersConnected: the host connects the externalized detection-builder plugins
	// (the core ensureBuildersConnected, byte-exact).
	dg.EnsureBuildersConnected = func(detected []string) error {
		return renderSeam(RenderSeamEnsureBuilders, EnsureBuildersParams{Dir: dir, Words: detected}, nil)
	}

	// ResolveDetectionBuilderStage: the host resolves + OpResolves a detection builder
	// (the core gen.resolveDetectionBuilderStageSeam, byte-exact).
	dg.ResolveDetectionBuilderStage = func(builderName string, in spec.BuilderResolveInput, img *buildkit.ResolvedBox) (spec.BuilderResolveReply, error) {
		var res DetectionBuilderResult
		if err := renderSeam(RenderSeamDetectionBuilder, DetectionBuilderParams{Dir: dir, BoxName: img.Name, BuilderName: builderName, In: in}, &res); err != nil {
			return spec.BuilderResolveReply{}, err
		}
		return res.Reply, nil
	}

	// ResolveExternalBuilderStage: the host resolves + OpResolves an external_builder-selected
	// out-of-tree builder (the core gen.resolveExternalBuilderStageSeam, byte-exact).
	dg.ResolveExternalBuilderStage = func(word, candyName string, img *buildkit.ResolvedBox) (spec.BuilderResolveReply, error) {
		var res ExternalBuilderResult
		if err := renderSeam(RenderSeamExternalBuilder, ExternalBuilderParams{Dir: dir, BoxName: img.Name, Word: word, CandyName: candyName}, &res); err != nil {
			return spec.BuilderResolveReply{}, err
		}
		return res.Reply, nil
	}

	// EmitBakedPlugins: via HostBuild("bake-plugins") — the host builds + stages
	// plugin binaries + returns the COPY/chmod fragment.
	dg.EmitBakedPlugins = func(b *strings.Builder, boxName string, candyOrder []string) error {
		reqJSON, err := json.Marshal(spec.BakePluginsRequest{
			Dir:        dir,
			BoxName:    boxName,
			CandyOrder: candyOrder,
		})
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
