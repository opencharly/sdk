package deploykit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// builders_render.go — the build-time BUILDER leg of the render engine (K3-A,
// relocated from charly/generate.go; byte-identical). The multi-stage builder
// render (detection builders pixi/npm/aur + `external_builder:` out-of-tree
// builders) operates over CandyModel + buildkit.ResolvedBox. Its ONLY host
// coupling — the provider registry (OpResolve Invoke + the on-demand connect) —
// is reached through the ResolveDetectionBuilderStage / ResolveExternalBuilderStage
// seam function fields (render_generator.go), which NewRenderGeneratorFromProject
// wires to plain InvokeProvider peer-dispatch. There is no EnsureBuildersConnected seam: the
// connect rides ops.InvokeProviderOpts.ExtraRef on the resolve Invoke itself. The two per-image
// reply caches
// (externalBuilderReplies / detectionBuilderReplies) live on Generator.

// EmitPreMainCandyStages emits the pre-main-FROM per-candy stages: the FROM-scratch
// COPY stages, the detection-builder multi-stage build stages (fully config-driven
// from the embedded builder: vocabulary), the EXTERNAL builder stages (a candy
// selecting an `external_builder:` gets its provider's OpResolve stage spliced
// pre-main-FROM; the artifacts COPY follows post-main-FROM via
// EmitExternalBuilderArtifacts), and the extraction stages.
func (g *Generator) EmitPreMainCandyStages(b *strings.Builder, boxName string, img *buildkit.ResolvedBox, candyOrder []string) error {
	g.EmitScratchStages(b, candyOrder)
	if err := g.EmitBuilderStages(b, boxName, img, candyOrder); err != nil {
		return err
	}
	if err := g.EmitExternalBuilderStages(b, img, candyOrder); err != nil {
		return err
	}
	g.EmitExtractStages(b, candyOrder)
	return nil
}

// EmitBuilderStages emits per-candy multi-stage build stages. Each builder declares
// detect_files, and a candy can declare packages for it under a distro tag (NeedsBuilder);
// for each candy that needs it an EXTERNALIZED builder (pixi/npm/aur/mise)
// renders its stage via its plugin's OpResolve leg (ResolveDetectionBuilderStage →
// kit.BuilderResolve -- not an in-core stage template); a non-externalized
// builder emits no build-time multi-stage (a custom builder must be an external_builder
// plugin). The externalized builder plugins are connected on-demand by the resolve Invoke itself
// (ops.InvokeProviderOpts.ExtraRef), so there is no separate EnsureBuildersConnected pre-pass
// and no second copy of the host's own connectPluginByWordRef.
// The reply is cached per (candy, builder) for EmitBuilderArtifacts.
func (g *Generator) EmitBuilderStages(b *strings.Builder, boxName string, img *buildkit.ResolvedBox, candyOrder []string) error {
	// FIRST, before any early return: every builder a candy needs on this image must be able to
	// run. requireBuilders is the ONE rule behind that refusal (opencharly/sdk#608).
	if err := g.requireBuilders(boxName, img, candyOrder); err != nil {
		return err
	}
	if img.BuilderConfig == nil {
		return nil // requireBuilders already refused any manager declaration this image selects
	}
	// Reset the per-image detection-builder reply cache (populated below, read by
	// EmitBuilderArtifacts). Scopes to ONE image, exactly like externalBuilderReplies.
	g.detectionBuilderReplies = map[string]spec.BuilderResolveReply{}

	for _, builderName := range img.BuilderConfig.BuilderNames() {
		builderDef := img.BuilderConfig.Builder[builderName]
		if builderDef.Inline {
			continue // an inline builder renders inside its candy's steps (candy_steps.go), not as a stage
		}
		for _, candyName := range candyOrder {
			layer := g.Candies[candyName]
			if !NeedsBuilder(layer, img, builderName, builderDef) {
				continue
			}
			builderRef := g.BuilderRefForFormat(boxName, builderName)
			reply, err := g.resolveDetectionBuilderReply(builderName, layer, builderDef, img, builderRef)
			if err != nil {
				return fmt.Errorf("image %q: candy %q builder %q: %w", boxName, candyName, builderName, err)
			}
			if strings.TrimSpace(reply.Stage) == "" {
				return fmt.Errorf("image %q: candy %q: builder %q returned an empty stage", boxName, candyName, builderName)
			}
			g.detectionBuilderReplies[BuilderCtxKey(layer.GetName(), builderName)] = reply
			b.WriteString(reply.Stage)
			b.WriteString("\n")
		}
	}
	return nil
}

// NeededBuilderImages lists, once each and sorted, the builder images the stage builders this
// image's candies need resolve to (BuilderRefForFormat over NeedsBuilder) — exactly the images
// EmitBuilderStages names in its FROM lines. A caller that renders those stages into a build whose
// builder images are not otherwise produced (the pod overlay) ensures each one first; the box
// build produces them as its own dependencies (opencharly/sdk#608).
func (g *Generator) NeededBuilderImages(boxName string, img *buildkit.ResolvedBox, candyOrder []string) []string {
	if img == nil || img.BuilderConfig == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range img.BuilderConfig.BuilderNames() {
		def := img.BuilderConfig.Builder[name]
		if def.Inline {
			continue // an inline builder runs inside the candy's own stage
		}
		for _, candyName := range candyOrder {
			if !NeedsBuilder(g.Candies[candyName], img, name, def) {
				continue
			}
			if ref := g.BuilderRefForFormat(boxName, name); ref != "" && !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
		}
	}
	sort.Strings(out)
	return out
}

// requireBuilders refuses an image on which a candy needs a builder that cannot run — the same
// rule for every builder (opencharly/sdk#608). For each builder in the vocabulary that a candy
// needs on this image (NeedsBuilder): an inline builder needs its plugin or an inline template; a
// stage builder needs its plugin and a route to a builder box. And a manager declaration this image
// selects needs a builder definition at all — checked even when the image resolved no builder
// vocabulary, the widest way a declaration could otherwise vanish.
func (g *Generator) requireBuilders(boxName string, img *buildkit.ResolvedBox, candyOrder []string) error {
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer == nil {
			continue
		}
		for _, manager := range declaredManagers(layer, img) {
			if img.BuilderConfig == nil || img.BuilderConfig.Builder[manager] == nil {
				return fmt.Errorf("image %q: candy %q declares %q packages, but no %q builder is defined; refusing to build an image that silently omits them",
					boxName, candyName, manager, manager)
			}
		}
		if img.BuilderConfig == nil {
			continue
		}
		for _, name := range img.BuilderConfig.BuilderNames() {
			def := img.BuilderConfig.Builder[name]
			if !NeedsBuilder(layer, img, name, def) {
				continue
			}
			external := g.ExternalizedBuilders[name]
			if def.Inline {
				if !external && buildkit.BuilderPhaseTemplate(def, spec.PhaseInstall, spec.VenueContainerBuilder) == "" {
					return fmt.Errorf("image %q: candy %q needs builder %q, which has neither a plugin nor an inline install template", boxName, candyName, name)
				}
				continue
			}
			if !external {
				return fmt.Errorf("image %q: candy %q needs builder %q, but its plugin is not available; refusing to build an image that silently omits its packages", boxName, candyName, name)
			}
			if g.BuilderRefForFormat(boxName, name) == "" {
				return fmt.Errorf("image %q: candy %q needs builder %q, but no builder box is routed for it (builder.%s)", boxName, candyName, name, name)
			}
		}
	}
	return nil
}

// declaredManagers lists the build-time managers a candy declares packages for on this image — the
// same tag chain its OS packages resolve over (ResolveManagerPackages), sorted for determinism.
func declaredManagers(layer CandyModel, img *buildkit.ResolvedBox) []string {
	seen := map[string]bool{}
	var out []string
	for _, tag := range CascadeTagChain(img) {
		cfg := layer.TagSection(tag)
		if cfg == nil {
			continue
		}
		for manager, ms := range cfg.Managers {
			if len(ms.Packages) > 0 && !seen[manager] {
				seen[manager] = true
				out = append(out, manager)
			}
		}
	}
	sort.Strings(out)
	return out
}

// resolveDetectionBuilderReply builds the OpResolve render context host-side
// (BuildStageContext + builderResolveInputFrom) for one (candy, builder) and
// dispatches through the ResolveDetectionBuilderStage seam (registry ResolveBuilder
// + OpResolve Invoke, charly-side). The counterpart of the external_builder path,
// sharing the SAME seam-served OpResolve Invoke (R3).
func (g *Generator) resolveDetectionBuilderReply(builderName string, layer CandyModel, builderDef *buildkit.BuilderDef, img *buildkit.ResolvedBox, builderRef string) (spec.BuilderResolveReply, error) {
	ctx, err := g.BuildStageContext(layer, builderName, builderDef, img, builderRef)
	if err != nil {
		return spec.BuilderResolveReply{}, err
	}
	in := BuilderResolveInputFrom(layer.GetName(), builderName, builderDef, ctx)
	return g.ResolveDetectionBuilderStage(builderName, in, &img.ResolvedBox)
}

// BuilderResolveInputFrom builds the serializable spec.BuilderResolveInput a builder plugin's
// OpResolve leg needs, from the host-computed BuildStageContext. RELOCATED to spec/spec
// (buildwire_render.go) so charly core can call it without an sdk/deploykit
// import — charly/tasks.go's inline-builder seam now calls spec.BuilderResolveInputFrom
// directly, shedding the LAST sdk-kit import. This deploykit re-export keeps the candy/plugin-
// installstep callers (stepEmitBuilder) byte-stable (R3: ONE source in spec/spec, re-exported
// here) — buildkit.BuilderDef is spec.Builder (alias), so the spec func's *spec.BuilderDef
// param typechecks against the *buildkit.BuilderDef the callers pass. Shared by the box-build
// path (resolveDetectionBuilderReply above) AND the pod-overlay build-emit
// (candy/plugin-installstep stepEmitBuilder) + the INLINE-builder resolve
// (NewRenderGeneratorFromProject's dg.ResolveInlineBuilder -- plugin-side), R3 -- hence the
// re-export.
var BuilderResolveInputFrom = spec.BuilderResolveInputFrom

// EmitExternalBuilderStages emits the pre-main-FROM multi-stage block for every
// candy that selects an `external_builder:` — the build-time BUILDER leg, the
// multi-stage counterpart of a `run:` step's `plugin:` verb. For each such candy it
// resolves the builder word through the ResolveExternalBuilderStage seam (registry
// ResolveBuilder + *grpcProvider assertion + OpResolve Invoke, charly-side, all error
// strings byte-preserved) and writes the returned BuilderResolveReply's Stage verbatim
// (egress-validated with the rest of the Containerfile). The reply is CACHED on the
// Generator so EmitExternalBuilderArtifacts can splice the matching COPY --from
// directives post-main-FROM without re-Invoking. The cache is RESET here so it scopes
// to ONE image.
func (g *Generator) EmitExternalBuilderStages(b *strings.Builder, img *buildkit.ResolvedBox, candyOrder []string) error {
	g.externalBuilderReplies = map[string]spec.BuilderResolveReply{}
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		word := layer.GetExternalBuilder()
		if word == "" {
			continue
		}
		reply, err := g.ResolveExternalBuilderStage(word, candyName, &img.ResolvedBox)
		if err != nil {
			return err
		}
		g.externalBuilderReplies[candyName] = reply
		b.WriteString(reply.Stage)
		if !strings.HasSuffix(reply.Stage, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return nil
}

// EmitExternalBuilderArtifacts writes the post-main-FROM COPY --from directives for
// each candy whose external builder resolved in EmitExternalBuilderStages (read from
// the per-image cache — never re-Invoked). The artifacts pull the built files out of
// the plugin's multi-stage build into the final image. A candy that selects an external builder
// whose stage was not resolved, or whose reply copies nothing, is an error: its build would reach
// no image (opencharly/sdk#608 — the same rule as every detection builder).
func (g *Generator) EmitExternalBuilderArtifacts(b *strings.Builder, candyOrder []string) error {
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer == nil || layer.GetExternalBuilder() == "" {
			continue
		}
		reply, ok := g.externalBuilderReplies[candyName]
		if !ok {
			return fmt.Errorf("candy %q selects external builder %q but no stage was resolved for it", candyName, layer.GetExternalBuilder())
		}
		if len(reply.CopyArtifacts) == 0 {
			return fmt.Errorf("candy %q: external builder %q returned no copy artifacts, so its build would reach no image", candyName, layer.GetExternalBuilder())
		}
		fmt.Fprintf(b, "# Copy external_builder artifacts (%s)\n", g.Candies[candyName].GetExternalBuilder())
		for _, line := range reply.CopyArtifacts {
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}
	return nil
}

// EmitBuilderArtifacts splices each needed builder's payload COPY lines (its cached OpResolve
// reply, populated by EmitBuilderStages) into the main image: the `# Copy <builder> artifacts`
// header once per builder, each candy's copies, and the builder BINARY once per builder. A builder
// whose PACKAGE output this image installs through its distro's local_pkg contract
// (BuilderPackageInstall) is installed by the candy's own steps instead, so its payload is not
// also copied. Every needed builder has a cached reply — a missing one is an error, never a
// silently absent payload (opencharly/sdk#608).
func (g *Generator) EmitBuilderArtifacts(b *strings.Builder, img *buildkit.ResolvedBox, candyOrder []string) error {
	if img.BuilderConfig == nil {
		return nil
	}
	for _, builderName := range img.BuilderConfig.BuilderNames() {
		builderDef := img.BuilderConfig.Builder[builderName]
		if builderDef.Inline {
			continue // an inline builder installs inside its candy's steps
		}
		hasArtifacts, binaryCopied := false, false
		for _, candyName := range candyOrder {
			layer := g.Candies[candyName]
			if !NeedsBuilder(layer, img, builderName, builderDef) {
				continue
			}
			reply, ok := g.detectionBuilderReplies[BuilderCtxKey(layer.GetName(), builderName)]
			if !ok {
				return fmt.Errorf("image %q: candy %q needs builder %q but no stage was resolved for it", img.Name, candyName, builderName)
			}
			if BuilderPackageInstall(img, reply.Package) == nil {
				for _, line := range reply.CopyArtifacts {
					if !hasArtifacts {
						fmt.Fprintf(b, "# Copy %s artifacts\n", builderName)
						hasArtifacts = true
					}
					b.WriteString(line)
					b.WriteString("\n")
				}
			}
			if reply.CopyBinary != "" && !binaryCopied {
				b.WriteString(reply.CopyBinary)
				b.WriteString("\n")
				binaryCopied = true
			}
		}
		if hasArtifacts || binaryCopied {
			b.WriteString("\n")
		}
	}
	return nil
}

// DetectExternalizedBuilders returns the builder words an image needs (NeedsBuilder over its
// candies), so a caller connects exactly those plugins on demand. The SAME trigger the render and
// the apply compile use — one rule (opencharly/sdk#608). A needed builder that is not externalized
// is returned too: connecting it then fails loudly, instead of the word being dropped here.
func DetectExternalizedBuilders(order []string, candies map[string]CandyModel, img *buildkit.ResolvedBox) []string {
	if img == nil || img.BuilderConfig == nil {
		return nil
	}
	var out []string
	for _, bName := range img.BuilderConfig.BuilderNames() {
		bDef := img.BuilderConfig.Builder[bName]
		for _, candyName := range order {
			if NeedsBuilder(candies[candyName], img, bName, bDef) {
				out = append(out, bName)
				break
			}
		}
	}
	return out
}
