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
// detect_files and/or detect_config (the embedded builder: vocabulary, host-side
// DETECTION); for each matching candy an EXTERNALIZED detection builder (pixi/npm/aur)
// renders its stage via its plugin's OpResolve leg (ResolveDetectionBuilderStage →
// kit.BuilderResolve -- not an in-core stage template); a non-externalized
// builder emits no build-time multi-stage (a custom builder must be an external_builder
// plugin). The externalized builder plugins are connected on-demand by the resolve Invoke itself
// (ops.InvokeProviderOpts.ExtraRef), so there is no separate EnsureBuildersConnected pre-pass
// and no second copy of the host's own connectPluginByWordRef.
// The reply is cached per (candy, builder) for EmitBuilderArtifacts.
func (g *Generator) EmitBuilderStages(b *strings.Builder, boxName string, img *buildkit.ResolvedBox, candyOrder []string) error {
	// FIRST, before any early return: every candy that DECLARES packages for a builder's
	// format must have a builder that can actually run. Checked ahead of the
	// `BuilderConfig == nil` return, because "no builders configured at all" is the most
	// complete way to suppress the error — a hidden default path whose only purpose would
	// be to drop the declaration silently.
	if err := g.requireDeclaredBuilders(boxName, img, candyOrder); err != nil {
		return err
	}
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
		// plugin's OpResolve (kit.BuilderResolve). A non-externalized builder emits no
		// stage here (a custom builder must be an external_builder plugin).
		//
		// A builder that cannot run because its plugin is not connected is a HARD ERROR
		// whenever a candy DECLARED packages for it. The previous `continue` dropped every
		// `aur:` package silently while the build reported success — measured on both the
		// arch and cachyos bases (opencharly/layer-kde-shell#7, opencharly/sdk#608).
		if !external {
			continue // requireDeclaredBuilders above already refused any declared-but-unrunnable builder
		}
		for _, candyName := range candyOrder {
			layer := g.Candies[candyName]
			// The candy needs this builder if a detect-file says so OR if it declares
			// packages for this builder's own format. The second arm is what keeps a
			// declared `aur: package: [...]` from being skipped when the detection is
			// file-based only (opencharly/sdk#608).
			if !g.CandyNeedsBuilder(img, layer, builderDef) && !declaresBuilderPackages(layer, builderName) {
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

// requireDeclaredBuilders refuses an image whose candies declare packages for a builder that
// cannot run: no builder definition at all, or no externalized (plugin) builder. It is the ONE
// rule behind that refusal, called before every early return in EmitBuilderStages
// (opencharly/sdk#608).
func (g *Generator) requireDeclaredBuilders(boxName string, img *buildkit.ResolvedBox, candyOrder []string) error {
	// Probe the union of: every builder the image CONFIGURES, and every builder format a
	// candy can DECLARE. The declaration set cannot be derived from the image — a box that
	// configures `pixi` while a candy declares `aur:` is exactly the case that would slip
	// through if only configured builders were probed (and a non-nil-but-empty Builder map
	// would probe nothing at all). The loader registers ONE format section today
	// (`ensureFormat("aur")` in loaderkit/scan_candy.go); `pixi` and `npm` are named here
	// because their builders exist, and a new format belongs in this list in the same change
	// that registers it.
	declaredFormats := []string{"aur", "pixi", "npm"}
	formats := append([]string{}, declaredFormats...)
	if img != nil && img.BuilderConfig != nil {
		seen := map[string]bool{}
		for _, f := range formats {
			seen[f] = true
		}
		for _, f := range img.BuilderConfig.BuilderNames() {
			if !seen[f] {
				formats = append(formats, f)
			}
		}
	}
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer == nil {
			continue
		}
		for _, format := range formats {
			if !declaresBuilderPackages(layer, format) {
				continue
			}
			var def *buildkit.BuilderDef
			if img != nil && img.BuilderConfig != nil {
				def = img.BuilderConfig.Builder[format]
			}
			if def == nil {
				return fmt.Errorf("image %q: candy %q declares %q packages, but no %q builder is configured; refusing to build an image that silently omits them", boxName, candyName, format, format)
			}
			if !g.ExternalizedBuilders[format] {
				return fmt.Errorf("image %q: candy %q declares %q packages, but builder %q is not available (its plugin is not connected); refusing to build an image that silently omits them", boxName, candyName, format, format)
			}
		}
	}
	return nil
}

// declaresBuilderPackages reports whether a candy declares packages for a builder's own
// format section (the `aur:`, `pixi:`, `npm:` arms) — the declaration a builder MUST honour.
// A candy that names packages here but gets no builder stage silently ships an image without
// them, which is the defect opencharly/sdk#608 records.
func declaresBuilderPackages(layer CandyModel, builderName string) bool {
	if layer == nil {
		return false
	}
	section := layer.FormatSection(builderName)
	return section != nil && len(section.Packages) > 0
}

// resolveDetectionBuilderReply builds the OpResolve render context host-side
// (BuildStageContext + builderResolveInputFrom) for one (candy, builder) and
// dispatches through the ResolveDetectionBuilderStage seam (registry ResolveBuilder
// + OpResolve Invoke, charly-side). The counterpart of the external_builder path,
// sharing the SAME seam-served OpResolve Invoke (R3).
func (g *Generator) resolveDetectionBuilderReply(builderName string, layer CandyModel, builderDef *buildkit.BuilderDef, img *buildkit.ResolvedBox, builderRef string) (spec.BuilderResolveReply, error) {
	ctx := g.BuildStageContext(layer, builderName, builderDef, img, builderRef)
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
// plugin owns the copy_artifact/copy_binary shape . For a custom (non-externalized)
// builder they come from the embedded builder: vocabulary copy_artifacts/copy_binary. Both
// keep the structure: the `# Copy <builder> artifacts` header once per builder, then each
// candy's artifacts, and the builder BINARY copied ONCE per builder (deduped across candies).
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
