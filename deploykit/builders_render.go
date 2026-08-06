package deploykit

import (
	"fmt"
	"slices"
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
// wires to plain InvokeProvider peer-dispatch. (The EnsureBuildersConnected seam
// that used to sit beside them is gone, K-wave 2 cone R1: the connect rides
// ops.InvokeProviderOpts.ExtraRef on the resolve Invoke itself.) The two per-image reply caches
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
// detect_files and/or detect_config (the embedded builder: vocabulary, host-side
// DETECTION); for each matching candy an EXTERNALIZED detection builder (pixi/npm/aur)
// renders its stage via its plugin's OpResolve leg (ResolveDetectionBuilderStage →
// kit.BuilderResolve, C10 — no longer an in-core stage template); a non-externalized
// builder emits no build-time multi-stage (a custom builder must be an external_builder
// plugin). The externalized builder plugins are connected on-demand by the resolve Invoke itself
// (ops.InvokeProviderOpts.ExtraRef — K-wave 2 cone R1 retired the separate EnsureBuildersConnected
// pre-pass, whose two-pass scan+load was a second copy of the host's own connectPluginByWordRef).
// The reply is cached per (candy, builder) for EmitBuilderArtifacts.
func (g *Generator) EmitBuilderStages(b *strings.Builder, boxName string, img *buildkit.ResolvedBox, candyOrder []string) error {
	if img.BuilderConfig == nil {
		return nil
	}
	// Reset the per-image detection-builder reply cache (populated below, read by
	// EmitBuilderArtifacts). Scopes to ONE image, exactly like externalBuilderReplies.
	g.detectionBuilderReplies = map[string]spec.BuilderResolveReply{}

	// Process builders in deterministic order
	builderNames := img.BuilderConfig.BuilderNames()
	for _, builderName := range builderNames {
		builderDef := img.BuilderConfig.Builder[builderName]
		if builderDef.Inline {
			continue // inline builders handled in writeCandySteps
		}
		external := g.ExternalizedBuilders[builderName]
		// Only an EXTERNALIZED (plugin) builder emits a build-time multi-stage — via its
		// plugin's OpResolve (kit.BuilderResolve, C10). A non-externalized builder emits no
		// stage here (a custom builder must be an external_builder plugin).
		if !external {
			continue
		}
		for _, candyName := range candyOrder {
			layer := g.Candies[candyName]
			if !g.CandyNeedsBuilder(img, layer, builderDef) {
				continue
			}
			builderRef := g.BuilderRefForFormat(boxName, builderName)
			if builderRef == "" {
				return fmt.Errorf("image %q: candy %q needs builder %q but no builders.%s configured", boxName, candyName, builderName, builderName)
			}
			reply, err := g.resolveDetectionBuilderReply(builderName, layer, builderDef, img, builderRef)
			if err != nil {
				return fmt.Errorf("image %q: candy %q builder %q: %w", boxName, candyName, builderName, err)
			}
			if strings.TrimSpace(reply.Stage) == "" {
				return fmt.Errorf("image %q: candy %q: detection builder %q returned an empty OpResolve stage", boxName, candyName, builderName)
			}
			g.detectionBuilderReplies[BuilderCtxKey(layer.GetName(), builderName)] = reply
			b.WriteString(reply.Stage)
			b.WriteString("\n")
		}
	}
	return nil
}

// resolveDetectionBuilderReply builds the OpResolve render context host-side
// (BuildStageContext + builderResolveInputFrom) for one (candy, builder) and
// dispatches through the ResolveDetectionBuilderStage seam (registry ResolveBuilder
// + OpResolve Invoke, charly-side). The C10 counterpart of the external_builder path,
// sharing the SAME seam-served OpResolve Invoke (R3).
func (g *Generator) resolveDetectionBuilderReply(builderName string, layer CandyModel, builderDef *buildkit.BuilderDef, img *buildkit.ResolvedBox, builderRef string) (spec.BuilderResolveReply, error) {
	ctx := g.BuildStageContext(layer, builderName, builderDef, img, builderRef)
	in := BuilderResolveInputFrom(layer.GetName(), builderName, builderDef, ctx)
	return g.ResolveDetectionBuilderStage(builderName, in, &img.ResolvedBox)
}

// BuilderResolveInputFrom builds the serializable spec.BuilderResolveInput a builder plugin's
// OpResolve leg needs, from the host-computed BuildStageContext. RELOCATED to spec/spec
// (buildwire_render.go, #55 coneK3tasks) so charly core can call it without an sdk/deploykit
// import — charly/tasks.go's inline-builder seam now calls spec.BuilderResolveInputFrom
// directly, shedding the LAST sdk-kit import. This deploykit re-export keeps the candy/plugin-
// installstep callers (stepEmitBuilder) byte-stable (R3: ONE source in spec/spec, re-exported
// here) — buildkit.BuilderDef is spec.Builder (alias), so the spec func's *spec.BuilderDef
// param typechecks against the *buildkit.BuilderDef the callers pass. Shared by the box-build
// path (resolveDetectionBuilderReply above) AND the pod-overlay build-emit
// (candy/plugin-installstep stepEmitBuilder) + the INLINE-builder resolve
// (NewRenderGeneratorFromProject's dg.ResolveInlineBuilder — plugin-side since K-wave 2 cone R1,
// where it replaced the deleted charly resolveInlineBuilderSeam), R3 — hence the re-export.
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
// the plugin's multi-stage build into the final image.
func (g *Generator) EmitExternalBuilderArtifacts(b *strings.Builder, candyOrder []string) {
	for _, candyName := range candyOrder {
		reply, ok := g.externalBuilderReplies[candyName]
		if !ok || len(reply.CopyArtifacts) == 0 {
			continue
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
}

// EmitBuilderArtifacts copies builder artifacts/binaries into the main image. For an
// EXTERNALIZED detection builder (pixi/npm/aur) the COPY lines come from the plugin's
// cached OpResolve reply (detectionBuilderReplies, populated by EmitBuilderStages) — the
// plugin owns the copy_artifact/copy_binary shape now (C10). For a custom (non-externalized)
// builder they come from the embedded builder: vocabulary copy_artifacts/copy_binary. Both
// preserve the former structure: the `# Copy <builder> artifacts` header once per builder,
// then each candy's artifacts, and the builder BINARY copied ONCE per builder (deduped
// across candies).
func (g *Generator) EmitBuilderArtifacts(b *strings.Builder, img *buildkit.ResolvedBox, candyOrder []string) {
	if img.BuilderConfig == nil {
		return
	}
	builderNames := img.BuilderConfig.BuilderNames()
	for _, builderName := range builderNames {
		builderDef := img.BuilderConfig.Builder[builderName]
		if builderDef.Inline {
			continue
		}
		external := g.ExternalizedBuilders[builderName]
		if !external {
			continue
		}

		// Find candies that triggered this builder
		hasArtifacts := false
		binaryCopied := false
		for _, candyName := range candyOrder {
			layer := g.Candies[candyName]
			if !g.CandyNeedsBuilder(img, layer, builderDef) {
				continue
			}

			if external {
				reply, ok := g.detectionBuilderReplies[BuilderCtxKey(layer.GetName(), builderName)]
				if !ok {
					continue
				}
				for _, line := range reply.CopyArtifacts {
					if !hasArtifacts {
						fmt.Fprintf(b, "# Copy %s artifacts\n", builderName)
						hasArtifacts = true
					}
					b.WriteString(line)
					b.WriteString("\n")
				}
				if reply.CopyBinary != "" && !binaryCopied {
					b.WriteString(reply.CopyBinary)
					b.WriteString("\n")
					binaryCopied = true
				}
				continue
			}

			stageName := fmt.Sprintf("%s-%s-build", layer.GetName(), builderName)

			// Copy artifacts
			for _, art := range builderDef.CopyArtifacts {
				if !hasArtifacts {
					fmt.Fprintf(b, "# Copy %s artifacts\n", builderName)
					hasArtifacts = true
				}
				src := expandBuilderPath(art.Src, img)
				dst := expandBuilderPath(art.Dst, img)
				if art.Chown {
					fmt.Fprintf(b, "COPY --from=%s --chown=%d:%d %s %s\n", stageName, img.UID, img.GID, src, dst)
				} else {
					fmt.Fprintf(b, "COPY --from=%s %s %s\n", stageName, src, dst)
				}
			}

			// Copy binary (only once, from first matching candy)
			if builderDef.CopyBinary != nil && !binaryCopied {
				fmt.Fprintf(b, "COPY --from=%s %s %s\n", stageName, builderDef.CopyBinary.Src, builderDef.CopyBinary.Dst)
				binaryCopied = true
			}
		}
		if hasArtifacts || binaryCopied {
			b.WriteString("\n")
		}
	}
}

// DetectExternalizedBuilders returns the externalized builder words an image
// triggers, so a caller can connect exactly those plugins on-demand. A config-only
// detection (aur) runs a distro-specific install_template, so it is only needed when
// the image's build formats include that format (the DetectConfig gate — a multi-distro
// candy's aur: section must NOT pull arch tooling onto a fedora build); a detect-files
// builder is needed by any candy carrying the file. Uses the img-less
// CandyNeedsBuilderStep (the DetectConfig BuildFormats gate is applied here, not in the
// step check). The SINGLE detection impl shared (R3) by the box-build RENDER
// (EmitBuilderStages, over the Generator's Candies + ExternalizedBuilders) AND the charly
// deploy build PRE-PASS (over its candy map + externalizedBuilders registry set).
func DetectExternalizedBuilders(order []string, candies map[string]CandyModel, externalized map[string]bool, img *buildkit.ResolvedBox) []string {
	if img == nil || img.BuilderConfig == nil {
		return nil
	}
	var out []string
	for _, bName := range img.BuilderConfig.BuilderNames() {
		if !externalized[bName] {
			continue
		}
		bDef := img.BuilderConfig.Builder[bName]
		if bDef == nil {
			continue
		}
		if bDef.DetectConfig != "" && !slices.Contains(img.BuildFormats, bDef.DetectConfig) {
			continue
		}
		for _, candyName := range order {
			if layer := candies[candyName]; layer != nil && CandyNeedsBuilderStep(layer, bDef) {
				out = append(out, bName)
				break
			}
		}
	}
	return out
}

// expandBuilderPath substitutes the {{.Home}} placeholder in a builder
// copy_artifact/copy_binary path (the only template token these paths carry).
func expandBuilderPath(path string, img *buildkit.ResolvedBox) string {
	path = strings.ReplaceAll(path, "{{.Home}}", img.Home)
	return path
}
