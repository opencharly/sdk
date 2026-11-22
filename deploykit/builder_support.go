package deploykit

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// BuildStageContext builds the render context for a candy's builder stage: stage
// names, copy source, user, cache mounts, detected manifest + lockfile-aware
// install command, manylinux fix, build script, and the manager packages this image selects.
// The copy source comes from the SAME origin→path mapping every other COPY site uses
// (candyCopySource), computed from the layer in hand: re-deriving the candy's map key here
// (CandyMapKey) yielded an empty key for a candy the resolver marked Remote without a
// RepoPath — an imported-namespace candy — so the copy-source lookup missed the layer.
// Relocated from charly (P8).
func (g *Generator) BuildStageContext(layer CandyModel, builderName string, builderDef *buildkit.BuilderDef, img *buildkit.ResolvedBox, builderRef string) (*spec.BuildStageContext, error) {
	stageName := fmt.Sprintf("%s-%s-build", layer.GetName(), builderName)
	ctx := &spec.BuildStageContext{
		BuilderRef:  builderRef,
		StageName:   stageName,
		LayerStage:  layer.GetName(),
		CopySrc:     g.candyCopySource(layer, layer.GetName()),
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
		// A render error is the builder's definition being broken — returned, never swallowed (a
		// swallowed one shipped the stage without its fix, opencharly/sdk#608).
		rendered, err := buildkit.RenderTemplate(builderName+"-manylinux", builderDef.ManylinuxFix, ctx)
		if err != nil {
			return nil, fmt.Errorf("candy %q: rendering builder %q manylinux_fix: %w", layer.GetName(), builderName, err)
		}
		ctx.ManylinuxFix = rendered
	}

	if builderDef.BuildScript != "" && layer.HasFile(builderDef.BuildScript) {
		ctx.HasBuildScript = true
		ctx.BuildScript = builderDef.BuildScript
	}

	ctx.Packages, ctx.Options = ResolveManagerPackages(layer, img, builderName)

	return ctx, nil
}

// CollectBuilderRuntimeEnv gathers the runtime env + PATH contributions declared by
// each builder a candy in the image triggers (via NeedsBuilder). Relocated (P8).
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
			if NeedsBuilder(layer, img, builderName, def) {
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

// NeedsBuilder is THE trigger rule, the same for every builder and both modes (opencharly/sdk#608):
// a candy needs builder `name` on an image when one of the builder's detect files is in the candy,
// OR the candy declares packages for that manager FOR THIS IMAGE (ResolveManagerPackages over the
// image's tag chain). There is no per-box opt-in: a declaration the image selects always needs its
// builder, and requireBuilders turns a builder that cannot run into a hard error.
func NeedsBuilder(layer CandyModel, img *buildkit.ResolvedBox, name string, def *buildkit.BuilderDef) bool {
	if layer == nil || def == nil {
		return false
	}
	for _, f := range def.DetectFiles {
		if layer.HasFile(f) {
			return true
		}
	}
	pkgs, _ := ResolveManagerPackages(layer, img, name)
	return len(pkgs) > 0
}

// BuilderCollectInputFor is the ONE constructor of a builder's collect-context input (the deploy
// pre-pass and the golden-compile tool both call it): the candy, the builder word, the image
// home, the manager packages this image selects (ResolveManagerPackages), and the candy's
// detected manifest with its text — what each builder derives its teardown from (D-IR-2). A
// manifest the candy carries but that cannot be read is an error, never an empty teardown.
func BuilderCollectInputFor(layer CandyModel, img *buildkit.ResolvedBox, word string, def *buildkit.BuilderDef) (spec.BuilderCollectInput, error) {
	in := spec.BuilderCollectInput{Candy: layer.GetName(), Builder: word}
	if img != nil {
		in.Home = img.Home
	}
	in.Packages, _ = ResolveManagerPackages(layer, img, word)
	if def != nil {
		for _, f := range def.DetectFiles {
			if !layer.HasFile(f) {
				continue
			}
			content, err := os.ReadFile(filepath.Join(layer.GetSourceDir(), f))
			if err != nil {
				return in, fmt.Errorf("candy %q: reading its %s manifest %s: %w", layer.GetName(), word, f, err)
			}
			in.Manifest, in.ManifestContent = f, string(content)
			break
		}
	}
	return in, nil
}

// BuilderPackageInstall returns the local-package contract that installs a builder's PACKAGE
// output on this image: the image's distro must serve exactly that package format with a
// `local_pkg` contract (pacman -U for `pac`). Nil means the builder's payload is what installs —
// the same as every builder without a package output. The package output is the builder PLUGIN's
// declaration (its resolve or collect reply, never the authored builder node — DESIGN.cue
// #BuilderAlt), and the format's install rule is the distro's data: one rule, for both modes
// (opencharly/sdk#608).
func BuilderPackageInstall(img *buildkit.ResolvedBox, pkg *spec.BuilderPackage) *spec.LocalPkg {
	if img == nil || pkg == nil || img.DistroDef == nil {
		return nil
	}
	if fd := img.DistroDef.Format[pkg.Format]; fd != nil {
		return fd.LocalPkg
	}
	return nil
}

// ResolveManagerPackages unions a candy's declarations for one build-time manager over the image's
// tag chain — the SAME chain and order its OS packages resolve with (CascadeTagChain,
// ResolveCascadePackages): a `distro.arch.aur:` block reaches an arch or cachyos image (whose chain
// carries `arch`) and no fedora one. Options come from the most specific level.
func ResolveManagerPackages(layer CandyModel, img *buildkit.ResolvedBox, manager string) (pkgs, options []string) {
	if layer == nil || img == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, tag := range slices.Backward(CascadeTagChain(img)) { // least → most specific
		cfg := layer.TagSection(tag)
		if cfg == nil {
			continue
		}
		ms, ok := cfg.Managers[manager]
		if !ok {
			continue
		}
		for _, p := range ms.Packages {
			if p != "" && !seen[p] {
				pkgs = append(pkgs, p)
				seen[p] = true
			}
		}
		if v := buildkit.ToStringSlice(ms.Raw["options"]); len(v) > 0 {
			options = v
		}
	}
	return pkgs, options
}
