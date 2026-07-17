package deploykit

import (
	"github.com/opencharly/sdk/spec"
)

// render_seam.go — the per-method param/result structs for the HostBuild("render-seam")
// dispatch (#67 render-DRIVE move). plugin-build wires each deploykit.Generator seam that STILL
// needs a host callback (EmitPluginOp, localpkg, inline-builder, ensure-builders) to
// HostBuild("render-seam", RenderSeamRequest{Method, Params}) where Params is ONE of the structs
// below marshalled to JSON. The host (host_build_render_seam.go) unmarshals Params by Method,
// calls the corresponding CORE function (byte-parity by construction — the EXACT funcs the core
// toDeploykit closures call), and returns RenderSeamReply{Result, Error} where Result is the
// marshalled result struct.
//
// K3 render-seam production move: RenderService, the two detection/external builder resolves,
// ValidateEgress, and RewriteHeaderCopy were PURE providerRegistry.resolve+Invoke dispatch (or,
// for RewriteHeaderCopy, pure data + host-fs I/O over data the CandyModel envelope already
// carries) — proven to need no host callback at all (RDD-spiked live on the external-builder
// leg) — so candy/plugin-build now calls them directly (render_generator_from_project.go) and
// their render-seam methods + param/result structs are GONE. The four REMAINING host-coupled
// seams below have a genuine host-only dependency: EnsureBuilders/InlineBuilder need the live
// loader's scan+connect machinery (rides K1, #40); EmitPluginOp needs a Go-level type-assertion
// (ProvisionActor/BuildEmitter) against a BUILTIN provider's concrete type, which only charly
// core holds (a builtin provider's generic Invoke() is an intentional error-stub — the perf
// invariant that lets a builtin skip the wire envelope, sdk/deploykit "InvokeProvider" doc).
//
// These structs live in deploykit (shared by charly core + candy/plugin-build, both of which
// import deploykit) and may reference spec types (BuilderDef = spec.Builder, BuildStageContext,
// Op — all CUE-sourced, json-tagged). They ride INSIDE the opaque RenderSeamRequest.Params bytes
// (a deploykit-internal dispatch detail, NOT a boundary-validated wire contract — the CUE wire
// type is RenderSeamRequest itself), so they are plain Go structs, not CUE-sourced. The one
// non-spec rich input — LocalPkgInstallStep (no json tags) — is NOT carried: the local-pkg
// method carries scalars + the host rebuilds the step via the SAME CompileLocalPkgStep the
// deploykit render used (byte-exact).

// RenderSeam method discriminators (the RenderSeamRequest.Method values).
const (
	RenderSeamInlineBuilder  = "inline-builder"
	RenderSeamLocalPkg       = "local-pkg"
	RenderSeamEnsureBuilders = "ensure-builders"
	RenderSeamEmitPluginOp   = "emit-plugin-op"
)

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
