package deploykit

import (
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/spec"
)

// Generator is the build-mode RENDER engine, homed in sdk/deploykit (P8).
//
// WHY deploykit and not buildkit: the render calls the build-compile helpers
// resolveCascadePackages / compileLocalPkgStep / compileShellSnippetSteps
// (sdk/deploykit/install_build.go) and reads deploykit.CandyModel. The import
// DAG is one-way deploykit→buildkit, so a buildkit-hosted render would cycle.
// deploykit already holds the build-compile foundation, so the render belongs
// here; buildkit stays the low keystone layer (ResolvedBox, RenderTemplate).
//
// The render operates over CandyModel (never the charly-side *Candy) and over
// buildkit.ResolvedBox. Every host-coupled dependency the render needs — the
// Config/Candy-graph label aggregators (CollectBox*/Collect*/Aggregate*), init
// resolution (ActiveInit/ResolveInitSystem), the registry/plugin Invoke legs
// (ensureBuildersConnected, OpResolve/OpEmit), the registry image probe
// (InspectImageUser), and the few Config.Defaults/BoxConfig reads — is threaded
// in as a function-value SEAM field (added compiler-driven as each render
// method is relocated onto this type). The charly-side resolve half
// (NewGenerator: LoadConfig/ScanAllCandy/loadProjectPlugins/ResolveAllBox/
// ComputeIntermediates/GlobalCandyOrder) constructs this value and wires the
// seams from the existing core functions; the podman orchestration
// (runBoxBuild/runBoxGenerate/push/retry/manifest) lives in candy/plugin-build
// and drives the build, calling back HostBuild for this host-side render.
type Generator struct {
	Dir            string
	Candies        map[string]CandyModel
	Tag            string
	Boxes          map[string]*buildkit.ResolvedBox
	BuildDir       string
	Containerfiles map[string]string // cached content per image (build pipes via stdin)
	GlobalOrder    []string          // popularity-weighted global candy order for cache reuse
	RequestedBoxes []string          // scopes which Containerfiles Generate() writes; empty = all

	// DevLocalPkg makes localpkg candies (the charly toolchain) build from LOCAL
	// in-development source instead of the published release — set ONLY for
	// disposable check-bed image builds. See renderLocalPkgImageInstall.
	DevLocalPkg bool

	// externalBuilderReplies caches each candy's external-builder OpResolve reply
	// for ONE image (Invoked once per candy); RESET per image.
	externalBuilderReplies map[string]spec.BuilderResolveReply
	// detectionBuilderReplies caches each (candy, builder) DETECTION-builder
	// OpResolve reply for ONE image (Invoked once per (candy, builder)); RESET per image.
	detectionBuilderReplies map[string]spec.BuilderResolveReply

	// --- HOST-COUPLED SEAMS (P8) ---
	// The render's host-side dependencies, threaded as function values by the core
	// resolve half (charly's g.toDeploykit()). Each wraps a core mechanism the render
	// reaches THROUGH the seam so no render-specific per-kind code stays in charly.

	// EmitPluginOp dispatches a non-"command" plugin verb op through the core
	// Provider registry: a ProvisionActor returns (script, true, nil) so emitTasks
	// emits it via EmitCmd; any other provider returns (fragment, false, nil) so
	// emitTasks writes it verbatim. The Provider registry + OpEmit dispatch (and its
	// error strings) STAY CORE; charly's toDeploykit() wires this closure. Op =
	// vmshared.Op = spec.Op.
	EmitPluginOp func(op *spec.Op, img *buildkit.ResolvedBox) (out string, isScript bool, err error)

	// CollectBoxPorts returns an image's aggregated exposed ports. Wraps the core
	// CollectBoxPorts(cfg, layers, boxName) aggregator (which reads the core Config
	// + Candy graph — RESOLVE-side, stays core). The first of the Collect* label
	// seams; the others (volumes/shell/security/hooks/descriptions/alias/caps) land
	// as their return types relocate to sdk (the writeLabels type-cascade).
	CollectBoxPorts func(boxName string) ([]string, error)

	// ValidateEgress gates a hand-built config (traefik routes, …) against the
	// egress CUE schemas before it is written into the build context. Wraps core
	// ValidateEgress (the egress validation stays core / its plugin) — mode "bytes".
	ValidateEgress func(kind, label string, data []byte) error

	// ValidateTextEgress gates a rendered TEXT artifact (the Containerfile — rejects the
	// "<no value>" template-failure marker) against the rendered_text constraint. Wraps core
	// validateTextEgress (kind "rendered_text", mode "text"). Distinct from ValidateEgress (bytes)
	// because the Containerfile is a rendered string, not a YAML/JSON blob (#67 render-DRIVE move).
	ValidateTextEgress func(label, text string) error

	// RenderService materializes a ServiceEntry into a RenderedService via
	// candy/plugin-init's OpResolve (the init-system template knowledge lives in
	// the plugin) and egress-gates the unit text. The plugin Invoke + the egress
	// gate STAY CORE; charly's toDeploykit() wires core RenderService here. Used by
	// GenerateInitFragments (fragment_assembly model).
	RenderService func(entry *spec.ServiceEntry, def *spec.ResolvedInit, ctx spec.ServiceRenderContext) (*spec.RenderedService, error)

	// RewriteHeaderCopyForRemote rewrites a stage_header_copy COPY line so its
	// source points at a materialized remote build-config asset (host fs write).
	// Wraps core rewriteHeaderCopyForRemote → materializeBuildConfigAsset (host
	// fs, stays core). Used by EmitInitFragmentStages.
	RewriteHeaderCopyForRemote func(headerCopy string) (string, error)

	// ExternalizedBuilders is the set of builder words whose inline render crosses
	// to a plugin's OpResolve (vs the embedded builder: vocabulary install_template).
	// Wired from core's externalizedBuilders registry fact. Read by WriteCandySteps
	// to pick the inline-builder branch.
	ExternalizedBuilders map[string]bool

	// RenderLocalPkgImageInstall renders a candy's localpkg OS-package install for
	// the image build. The PRODUCTION path is pure render (a curl+install RUN); the
	// DevLocalPkg path builds the in-development package on the HOST (makepkg). Wraps
	// core renderLocalPkgImageInstall (host-coupled dev leg stays core). Used by
	// WriteCandySteps.
	RenderLocalPkgImageInstall func(step *LocalPkgInstallStep, devLocalPkg bool, imageDir, boxName string) (string, error)

	// ResolveInlineBuilder connects + OpResolves an externalized INLINE builder and
	// returns its in-candy fragment (C10 InlineFragment). Wraps the core builder-emit
	// cluster (ensureBuildersConnected + registry ResolveBuilder + resolveBuilderStage;
	// registry-coupled, stays core), preserving its per-failure error strings. Used by
	// WriteCandySteps.
	ResolveInlineBuilder func(candyName, builderName string, bDef *buildkit.BuilderDef, ctx *spec.BuildStageContext, img *buildkit.ResolvedBox) (inlineFragment string, err error)

	// EnsureBuildersConnected connects the EXTERNALIZED detection-builder plugins an
	// image triggers (on-demand, scoped — the SAME machinery the deploy build PRE-PASS
	// uses, R3), so their OpResolve build leg can be Invoked. Wraps core
	// ensureBuildersConnected(ctx, cfg, dir, detected) (registry-coupled, stays core).
	// Used by EmitBuilderStages.
	EnsureBuildersConnected func(detected []string) error

	// ResolveDetectionBuilderStage resolves + Invokes an externalized DETECTION-builder
	// plugin's OpResolve build leg for one (candy, builder), returning the rendered
	// BuilderResolveReply. deploykit builds the render context (BuildStageContext +
	// builderResolveInputFrom) and passes the serializable input; the seam does the
	// registry ResolveBuilder + OpResolve Invoke (its "not connected" error preserved
	// byte-exact). Wraps core resolveDetectionBuilder's registry half. Used by EmitBuilderStages.
	ResolveDetectionBuilderStage func(builderName string, in spec.BuilderResolveInput, img *buildkit.ResolvedBox) (spec.BuilderResolveReply, error)

	// ResolveExternalBuilderStage resolves + Invokes an `external_builder:`-selected
	// out-of-tree builder provider's OpResolve, returning the reply (non-empty Stage
	// required). The seam does registry ResolveBuilder + the *grpcProvider assertion +
	// the minimal-input Invoke (all its error strings preserved byte-exact). Wraps core
	// resolveExternalBuilder + its provider resolution. Used by EmitExternalBuilderStages.
	ResolveExternalBuilderStage func(word, candyName string, img *buildkit.ResolvedBox) (spec.BuilderResolveReply, error)

	// EmitBakedPlugins bakes each composing candy's `bake_plugin:` out-of-tree plugin
	// binaries into the FINAL image at /usr/lib/charly/plugins/. The deploykit render
	// calls this seam post-main-FROM (after EmitExternalBuilderArtifacts). The host
	// (live path) wires the charly emitBakedPlugins closure; plugin-build wires it via
	// HostBuild("bake-plugins"). Used by generateContainerfile (#67 render-DRIVE move).
	EmitBakedPlugins func(b *strings.Builder, boxName string, candyOrder []string) error

	// CollectBoxVolume returns an image's aggregated volume mounts. Wraps the core
	// CollectBoxVolume(cfg, layers, boxName, home, nil) aggregator (which reads the core
	// Config + Candy graph — RESOLVE-side, stays core). Used by generateDataImageContainerfile.
	CollectBoxVolume func(boxName string, home string) ([]VolumeMount, error)
}

// NewRenderGenerator constructs a render Generator with its unexported per-image
// reply caches initialized. The caller (charly's g.toDeploykit()) sets the
// exported data fields + seam function values directly on the returned value —
// the caches cannot be set cross-package, which is the whole reason this
// constructor exists.
func NewRenderGenerator() *Generator {
	return &Generator{
		externalBuilderReplies:  make(map[string]spec.BuilderResolveReply),
		detectionBuilderReplies: make(map[string]spec.BuilderResolveReply),
	}
}
