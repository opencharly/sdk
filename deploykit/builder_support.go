package deploykit

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// BuildStageContext builds the render context for a candy's builder stage: stage
// names, copy source, user, cache mounts, detected manifest + lockfile-aware
// install command, manylinux fix, build script, and config-detected packages.
// Relocated from charly (P8); byte-identical.
func (g *Generator) BuildStageContext(layer CandyModel, builderName string, builderDef *buildkit.BuilderDef, img *buildkit.ResolvedBox, builderRef string) *spec.BuildStageContext {
	stageName := fmt.Sprintf("%s-%s-build", layer.GetName(), builderName)
	ctx := &spec.BuildStageContext{
		BuilderRef:  builderRef,
		StageName:   stageName,
		LayerStage:  layer.GetName(),
		CopySrc:     g.CandyCopySource(CandyMapKey(layer)),
		UID:         img.UID,
		GID:         img.GID,
		Home:        img.Home,
		User:        img.User,
		CacheMounts: builderDef.CacheMount,
	}

	if len(builderDef.InstallCommands) > 0 && len(builderDef.DetectFiles) > 0 {
		manifest := ""
		for _, f := range builderDef.DetectFiles {
			if layer.HasFile(f) {
				manifest = f
				break
			}
		}
		ctx.Manifest = manifest
		ctx.HasLockFile = kit.FileExists(filepath.Join(layer.GetSourceDir(), manifest+".lock")) ||
			(manifest == "pixi.toml" && layer.GetHasPixiLock())

		if ctx.HasLockFile {
			if cmd, ok := builderDef.InstallCommands[manifest+"+lock"]; ok {
				ctx.InstallCmd = cmd
			}
		}
		if ctx.InstallCmd == "" {
			if cmd, ok := builderDef.InstallCommands[manifest]; ok {
				ctx.InstallCmd = cmd
			}
		}
	}

	if builderDef.ManylinuxFix != "" && ctx.Manifest != "" {
		rendered, err := buildkit.RenderTemplate(builderName+"-manylinux", builderDef.ManylinuxFix, ctx)
		if err == nil {
			ctx.ManylinuxFix = rendered
		}
	}

	if builderDef.BuildScript != "" && layer.HasFile(builderDef.BuildScript) {
		ctx.HasBuildScript = true
		ctx.BuildScript = builderDef.BuildScript
	}

	if builderDef.DetectConfig != "" {
		section := layer.FormatSection(builderDef.DetectConfig)
		if section != nil {
			ctx.Packages = section.Packages
			ctx.Options = buildkit.ToStringSlice(section.Raw["options"])
		}
	}

	return ctx
}

// CollectBuilderRuntimeEnv gathers the runtime env + PATH contributions declared by
// each builder a candy in the image triggers (via CandyNeedsBuilder). Relocated (P8).
func (g *Generator) CollectBuilderRuntimeEnv(candyOrder []string, img *buildkit.ResolvedBox) []*kit.EnvConfig {
	if img == nil || img.BuilderConfig == nil {
		return nil
	}
	var out []*kit.EnvConfig
	for _, builderName := range img.BuilderConfig.BuilderNames() {
		def := img.BuilderConfig.Builder[builderName]
		if def == nil {
			continue
		}
		if len(def.RuntimeEnv) == 0 && len(def.PathContributions) == 0 {
			continue
		}
		triggered := false
		for _, candyName := range candyOrder {
			layer, ok := g.Candies[candyName]
			if !ok {
				continue
			}
			if g.CandyNeedsBuilder(img, layer, def) {
				triggered = true
				break
			}
		}
		if !triggered {
			continue
		}
		out = append(out, &kit.EnvConfig{
			Vars:       def.RuntimeEnv,
			PathAppend: def.PathContributions,
		})
	}
	return out
}

// CandyNeedsBuilder returns true if a candy triggers a builder: a DetectFiles match
// always triggers; a DetectConfig match triggers only when the image's build formats
// include that format (a config-only detection is distro-coupled — the IR compiler
// only emits install steps for formats in img.BuildFormats). Relocated from charly
// (P8); byte-identical (buildFormatsInclude inlined as slices.Contains).
func (g *Generator) CandyNeedsBuilder(img *buildkit.ResolvedBox, layer CandyModel, builderDef *buildkit.BuilderDef) bool {
	for _, f := range builderDef.DetectFiles {
		if layer.HasFile(f) {
			return true
		}
	}
	if builderDef.DetectConfig != "" {
		section := layer.FormatSection(builderDef.DetectConfig)
		if section != nil && len(section.Packages) > 0 {
			if img != nil && !slices.Contains(img.BuildFormats, builderDef.DetectConfig) {
				return false
			}
			return true
		}
	}
	return false
}
