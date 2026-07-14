package deploykit

import (
	"github.com/opencharly/sdk/spec"
)

// render_seam.go — the per-method param/result structs for the HostBuild("render-seam")
// dispatch (#67 render-DRIVE move). plugin-build wires each deploykit.Generator seam that
// needs a host callback (RenderService, the builder resolves, ValidateEgress, EmitPluginOp,
// localpkg, header-copy, ensure-builders) to HostBuild("render-seam", RenderSeamRequest{Method,
// Params}) where Params is ONE of the structs below marshalled to JSON. The host
// (host_build_render_seam.go) unmarshals Params by Method, calls the corresponding CORE
// function (byte-parity by construction — the EXACT funcs the core toDeploykit closures call),
// and returns RenderSeamReply{Result, Error} where Result is the marshalled result struct.
//
// These structs live in deploykit (shared by charly core + candy/plugin-build, both of which
// import deploykit) and may reference spec types (BuilderDef = spec.Builder, BuildStageContext,
// ServiceEntry, ResolvedInit, ServiceRenderContext, Op — all CUE-sourced, json-tagged). They
// ride INSIDE the opaque RenderSeamRequest.Params bytes (a deploykit-internal dispatch detail,
// NOT a boundary-validated wire contract — the CUE wire type is RenderSeamRequest itself), so
// they are plain Go structs, not CUE-sourced. The one non-spec rich input — LocalPkgInstallStep
// (no json tags) — is NOT carried: the local-pkg method carries scalars + the host rebuilds the
// step via the SAME CompileLocalPkgStep the deploykit render used (byte-exact).

// RenderSeam method discriminators (the RenderSeamRequest.Method values).
const (
	RenderSeamRenderService     = "render-service"
	RenderSeamDetectionBuilder  = "detection-builder"
	RenderSeamExternalBuilder   = "external-builder"
	RenderSeamInlineBuilder     = "inline-builder"
	RenderSeamLocalPkg          = "local-pkg"
	RenderSeamRewriteHeaderCopy = "rewrite-header-copy"
	RenderSeamEnsureBuilders    = "ensure-builders"
	RenderSeamEmitPluginOp      = "emit-plugin-op"
	RenderSeamValidateEgress    = "validate-egress"
)

// RenderServiceParams carries the inputs to core RenderService.
type RenderServiceParams struct {
	Dir     string                    `json:"dir"`
	BoxName string                    `json:"box_name"`
	Entry   *spec.ServiceEntry        `json:"entry"`
	Def     *spec.ResolvedInit        `json:"def"`
	Ctx     spec.ServiceRenderContext `json:"ctx"`
}

// RenderServiceResult carries the RenderedService.
type RenderServiceResult struct {
	Rendered *spec.RenderedService `json:"rendered"`
}

// DetectionBuilderParams carries the inputs to core resolveDetectionBuilderStageSeam.
type DetectionBuilderParams struct {
	Dir         string                   `json:"dir"`
	BoxName     string                   `json:"box_name"`
	BuilderName string                   `json:"builder_name"`
	In          spec.BuilderResolveInput `json:"in"`
}

// DetectionBuilderResult carries the BuilderResolveReply.
type DetectionBuilderResult struct {
	Reply spec.BuilderResolveReply `json:"reply"`
}

// ExternalBuilderParams carries the inputs to core resolveExternalBuilderStageSeam.
type ExternalBuilderParams struct {
	Dir       string `json:"dir"`
	BoxName   string `json:"box_name"`
	Word      string `json:"word"`
	CandyName string `json:"candy_name"`
}

// ExternalBuilderResult carries the BuilderResolveReply.
type ExternalBuilderResult struct {
	Reply spec.BuilderResolveReply `json:"reply"`
}

// InlineBuilderParams carries the inputs to core resolveInlineBuilderSeam. BDef is spec.Builder
// (BuilderDef = spec.Builder); Ctx is spec.BuildStageContext — both CUE-sourced, json-tagged.
type InlineBuilderParams struct {
	Dir         string                  `json:"dir"`
	BoxName     string                  `json:"box_name"`
	CandyName   string                  `json:"candy_name"`
	BuilderName string                  `json:"builder_name"`
	BDef        *spec.Builder           `json:"b_def"`
	Ctx         *spec.BuildStageContext `json:"ctx"`
}

// InlineBuilderResult carries the inline fragment.
type InlineBuilderResult struct {
	Fragment string `json:"fragment"`
}

// LocalPkgParams carries the SCALAR inputs to core renderLocalPkgImageInstall. The
// LocalPkgInstallStep itself is NOT carried (no json tags); the host rebuilds it via
// CompileLocalPkgStep(gen.Candies[candyName], gen.Boxes[boxName], HostContext{}) — the SAME
// call the deploykit render makes (candy_steps.go), so byte-exact.
type LocalPkgParams struct {
	Dir         string `json:"dir"`
	BoxName     string `json:"box_name"`
	CandyName   string `json:"candy_name"`
	ImageDir    string `json:"image_dir"`
	DevLocalPkg bool   `json:"dev_local_pkg"`
}

// LocalPkgResult carries the rendered localpkg image-install fragment.
type LocalPkgResult struct {
	Fragment string `json:"fragment"`
}

// RewriteHeaderCopyParams carries the header-copy string to core rewriteHeaderCopyForRemote.
type RewriteHeaderCopyParams struct {
	Dir        string `json:"dir"`
	BoxName    string `json:"box_name"`
	HeaderCopy string `json:"header_copy"`
}

// RewriteHeaderCopyResult carries the rewritten header copy.
type RewriteHeaderCopyResult struct {
	HeaderCopy string `json:"header_copy"`
}

// EnsureBuildersParams carries the builder words to core ensureBuildersConnected.
type EnsureBuildersParams struct {
	Dir   string   `json:"dir"`
	Words []string `json:"words"`
}

// EmitPluginOpParams carries the plugin-verb op to the core EmitPluginOp logic (the
// providerRegistry.ResolveVerb + ProvisionActor/OpEmit dispatch). The img (for distros) is read
// from the cached Generator's gen.Boxes[box_name].
type EmitPluginOpParams struct {
	Dir     string   `json:"dir"`
	BoxName string   `json:"box_name"`
	Op      *spec.Op `json:"op"`
}

// EmitPluginOpResult carries the rendered fragment + whether it is an act-script.
type EmitPluginOpResult struct {
	Out      string `json:"out"`
	IsScript bool   `json:"is_script"`
}

// ValidateEgressParams carries the inputs to core egressValidate (the unexported core dispatcher
// behind ValidateEgress/validateTextEgress/ValidateXMLEgress). Mode is "bytes" (YAML/JSON, e.g.
// traefik routes), "text" (rendered_text — the Containerfile + service units), or "xml". The host
// calls egressValidate(Kind, Label, Mode, string(Data)) — one path for every egress call site.
type ValidateEgressParams struct {
	Kind  string `json:"kind"`
	Mode  string `json:"mode"`
	Label string `json:"label"`
	Data  []byte `json:"data"`
}
