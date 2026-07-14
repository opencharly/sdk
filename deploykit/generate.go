package deploykit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// generate.go — the build-mode Containerfile RENDER DRIVE, homed in sdk/deploykit (#67
// render-DRIVE move). Relocated from charly/generate.go: the Generate loop +
// generateContainerfile + generateDataImageContainerfile read PRE-FILLED build-render
// CACHES on *buildkit.ResolvedBox (RenderCandyOrder/CandyCaps/ActiveInits/InitSystem/
// InitDef/BakedMetadata — filled by the host render-prep or re-attached by
// NewSpecResolvedBox from the resolved-project envelope) + CandyModel + the host-coupled
// SEAM fields on Generator. NO live *Candy/*Config/*InitConfig reads — a pure
// orchestrator over resolved data. The host render-prep (charly/render_prep.go) fills the
// caches; plugin-build constructs the Generator from the envelope + wires the seams.

// Generate renders a Containerfile for each box in order, writing them to
// g.BuildDir/<name>/Containerfile and caching the content in g.Containerfiles.
// The caller (charly's hostBuildBuildResolve generate-prep, or plugin-build's
// runBoxGenerate/runBoxBuild) provides the order; the build-PREP (cleanStaleBuildDirs,
// MkdirAll, writeContextIgnore, createRemoteCandyCopies, ResolveBoxOrder, filterBox,
// resolveUserContext) stays HOST — Generate does ONLY the per-box render.
func (g *Generator) Generate(order []string) error {
	for _, name := range order {
		if err := g.generateContainerfile(name); err != nil {
			return fmt.Errorf("generating Containerfile for %s: %w", name, err)
		}
	}
	return nil
}

// generateContainerfile generates a Containerfile for a single image, reading
// the pre-filled build-render caches on img. The caps/init aggregation is
// REMOVED (caches pre-filled by render-prep); writeLabels → WriteLabels (reads
// img.BakedMetadata); emitBakedPlugins → the EmitBakedPlugins seam.
func (g *Generator) generateContainerfile(boxName string) error {
	imageDir := filepath.Join(g.BuildDir, boxName)

	img := g.Boxes[boxName]
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# .build/%s/Containerfile (generated -- do not edit)\n\n", boxName)

	// candyOrder is PRE-FILLED by render-prep (host render-prep or NewSpecResolvedBox
	// re-attach). NO recomputation.
	candyOrder := img.RenderCandyOrder

	// Data images: minimal FROM scratch with only data staging + labels
	if img.DataImage {
		return g.generateDataImageContainerfile(boxName, img, candyOrder, imageDir)
	}

	// ARG for base image must come first (before any FROM).
	resolvedBase := g.ResolveBaseImage(img)
	fmt.Fprintf(&b, "ARG BASE_IMAGE=%s\n\n", resolvedBase)

	// Emit the pre-main-FROM per-candy stages: scratch COPY stages, detection-builder
	// multi-stage build stages, external-builder stages, and extraction stages.
	if err := g.EmitPreMainCandyStages(&b, boxName, img, candyOrder); err != nil {
		return err
	}

	// activeInits is PRE-FILLED by render-prep. NO recomputation.
	activeInits := img.ActiveInits

	// Detect route/traefik candies and emit the traefik-routes scratch stage.
	hasRoutes, hasTraefik, err := g.EmitTraefikRouteStage(&b, boxName, img, candyOrder)
	if err != nil {
		return err
	}

	// Emit init system stages and learn which inits received fragment content.
	initHasFragments, err := g.EmitInitFragmentStages(&b, boxName, img, candyOrder, activeInits)
	if err != nil {
		return err
	}

	// Main image
	b.WriteString("FROM ${BASE_IMAGE}\n\n")

	// Import the from-builder rootfs (if any) and reset the base for candy processing.
	g.EmitBaseBootstrap(&b, boxName, img)

	// Collect and write environment variables from candies
	g.WriteCandyEnv(&b, candyOrder, img)

	// Emit EXPOSE directives for the box's inherited candy ports
	g.WriteExpose(&b, img.Name)

	// Copy builder artifacts — fully config-driven from the embedded builder: vocabulary
	g.EmitBuilderArtifacts(&b, img, candyOrder)

	// Copy EXTERNAL builder artifacts — the cached OpResolve reply's COPY --from directives.
	g.EmitExternalBuilderArtifacts(&b, candyOrder)

	// Bake out-of-tree `bake_plugin:` provider binaries into the FINAL image.
	if err := g.EmitBakedPlugins(&b, boxName, candyOrder); err != nil {
		return err
	}

	// Copy extracted files from multi-stage builds
	g.EmitExtractedFiles(&b, img, candyOrder)

	// Stage data files from data candies into /data/ for deploy-time provisioning
	g.WriteDataStaging(&b, candyOrder, img)

	// Process each candy. Post-candy steps (init assembly, traefik, bootc) run as root,
	// so the last candy must reset to root only if such steps exist.
	caps := img.CandyCaps
	needsRootAfter := len(activeInits) > 0 || (hasRoutes && hasTraefik) || (caps != nil && caps.NeedsRootAfterInit)
	inUserMode := false
	for i, candyName := range candyOrder {
		isLast := i == len(candyOrder)-1
		var werr error
		inUserMode, werr = g.WriteCandySteps(&b, candyName, img, isLast && !needsRootAfter)
		if werr != nil {
			return werr
		}
	}

	// Assemble init system configs (driven by the embedded init: vocabulary templates)
	if err := g.EmitInitAssembly(&b, img, candyOrder, activeInits, initHasFragments); err != nil {
		return err
	}

	// Copy traefik dynamic routes if needed
	if hasRoutes && hasTraefik {
		b.WriteString("# Traefik dynamic routes\n")
		b.WriteString("COPY --from=traefik-routes /routes.yml /etc/traefik/dynamic/routes.yml\n\n")
	}

	// Final USER directive (use UID for robustness)
	if caps != nil && caps.PreserveUser {
		// leave as root — systemd handles user sessions
	} else if !inUserMode || needsRootAfter {
		fmt.Fprintf(&b, "USER %d\n", img.UID)
	}

	// Emit image metadata labels LAST. img.BakedMetadata is PRE-FILLED by render-prep
	// (buildBakedMetadata). WriteLabels FORMATS it byte-for-byte.
	g.WriteLabels(&b, img.BakedMetadata, boxName)

	// Ensure the image dir exists.
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return err
	}

	content := b.String()
	g.Containerfiles[boxName] = content

	containerfile := filepath.Join(imageDir, "Containerfile")
	return g.writeContainerfile(containerfile, content)
}

// generateDataImageContainerfile produces a minimal FROM scratch Containerfile
// with only data staging COPY instructions and OCI labels. No runtime, no init,
// no packages, no builder stages. Reads CandyModel + the CollectBoxVolume seam.
func (g *Generator) generateDataImageContainerfile(boxName string, img *buildkit.ResolvedBox, candyOrder []string, imageDir string) error {
	var b strings.Builder

	fmt.Fprintf(&b, "# .build/%s/Containerfile (generated -- do not edit)\n\n", boxName)
	b.WriteString("FROM scratch\n\n")

	// Scratch stages for candies that have data
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if !layer.HasData() {
			continue
		}
		fmt.Fprintf(&b, "FROM scratch AS %s\n", layer.GetName())
		fmt.Fprintf(&b, "COPY %s/ /\n\n", g.CandyCopySource(candyName))
	}

	// Main image: just data staging + labels
	b.WriteString("FROM scratch\n\n")

	// Data staging COPY instructions
	g.WriteDataStaging(&b, candyOrder, img)

	// Minimal labels (no init, no services, no ports). Content-derived
	// EffectiveVersion (not the per-build tag).
	b.WriteString("# Image metadata\n")
	fmt.Fprintf(&b, "LABEL %s=%q\n", spec.LabelVersion, img.EffectiveVersion)
	fmt.Fprintf(&b, "LABEL %s=%q\n", spec.LabelBox, boxName)
	if img.Registry != "" {
		fmt.Fprintf(&b, "LABEL %s=%q\n", spec.LabelRegistry, img.Registry)
	}
	fmt.Fprintf(&b, "LABEL %s=%q\n", spec.LabelDataBox, "true")

	// Data entries label
	var dataEntries []spec.LabelDataEntry
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if !layer.HasData() {
			continue
		}
		for _, d := range layer.Data() {
			staging := "/data/" + d.Volume + "/"
			if d.Dest != "" {
				staging += d.Dest
				if !strings.HasSuffix(staging, "/") {
					staging += "/"
				}
			}
			dataEntries = append(dataEntries, spec.LabelDataEntry{
				Volume:  d.Volume,
				Staging: staging,
				Candy:   candyName,
				Dest:    d.Dest,
			})
		}
	}
	if len(dataEntries) > 0 {
		writeJSONLabel(&b, spec.LabelDataEntries, dataEntries)
	}

	// Volume labels (so charly config knows what volumes data targets)
	volumes, _ := g.CollectBoxVolume(boxName, img.Home)
	if len(volumes) > 0 {
		labelVols := make([]spec.LabelVolumeEntry, 0, len(volumes))
		for _, v := range volumes {
			shortName := strings.TrimPrefix(v.VolumeName, "charly-"+boxName+"-")
			labelVols = append(labelVols, spec.LabelVolumeEntry{Name: shortName, Path: v.ContainerPath})
		}
		writeJSONLabel(&b, spec.LabelVolume, labelVols)
	}

	// Candy versions
	candyVersions := make(map[string]string)
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer.GetVersion() != "" {
			candyVersions[candyName] = layer.GetVersion()
		}
	}
	writeJSONLabel(&b, spec.LabelCandyVersion, candyVersions)

	b.WriteString("\n")

	// Write to disk
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return err
	}
	content := b.String()
	g.Containerfiles[boxName] = content
	return g.writeContainerfile(filepath.Join(imageDir, "Containerfile"), content)
}

// writeContainerfile validates the rendered Containerfile (catching Go-template
// render failures via the egress seam) and writes it atomically.
func (g *Generator) writeContainerfile(path, content string) error {
	if g.ValidateTextEgress != nil {
		if err := g.ValidateTextEgress(path, content); err != nil {
			return err
		}
	}
	return kit.AtomicWriteFile(path, []byte(content), 0644)
}
