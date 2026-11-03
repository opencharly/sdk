package deploykit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildcache"
	"github.com/opencharly/sdk/buildkit"
)

// bake_plugins.go — the S0 baked-plugin BUILD-side seam (K3 build-tail move, coneB-buildtail):
// bakes each composing candy's `bake_plugin:` out-of-tree plugin binaries into the FINAL image at
// bakedPluginDir (/usr/lib/charly/plugins/), so a DEPLOYED container (which has neither the candy
// source nor a go toolchain) can run an external plugin its in-container charly needs at runtime.
//
// Relocated from charly/generate.go's (*Generator).emitBakedPlugins. THIS FILE NOW HOLDS ONLY THE
// CONSTANTS AND THE EMIT LOGIC, wired by NewRenderGeneratorFromProject. The BUILD it used to carry
// moved to sdk/buildcache in this PR, so THIS path calls the one implementation; the runtime plugin
// loader still owns its own cluster AT THIS COMMIT and is repointed onto the same implementation in
// the pair's SECOND PR (see below).
//
// DELETED FROM HERE (this cutover, sdk PR): the local buildPluginBinary copy and its
// safePluginBinName/pluginBuildCacheDir/pluginSourceTag helpers, plus the five helpers that existed
// only to serve that build. The former "bake-plugins" HostBuild round-trip
// (charly/host_build_bake_plugins.go) was already deleted earlier: EmitBakedPlugins runs
// in-process, with zero host round-trip.
//
// The package-main cluster in charly/charly/plugin_loader.go (which the old comment here called a
// necessary duplicate, because the two modules cannot share private code and the runtime plugin
// LOADER needs its own bakedPluginBinary fallback) is deleted in the pair's SECOND PR, together
// with the repoint of its two build arms onto buildcache.Build.

// bakedPluginDir is the FHS system path a candy's `bake_plugin:` step copies a pre-built provider
// binary to at image-build time. Byte-identical to charly/plugin_loader.go's constant.
const bakedPluginDir = "/usr/lib/charly/plugins"

// bakedPluginImageDir is where an IMAGE bake writes its provider binaries — deliberately NOT
// bakedPluginDir, which the DISTRO PACKAGE owns. charly's own package installs
// /usr/lib/charly/plugins/** and its file list is authoritative there, so a build step writing
// into that directory collides with the package transaction that installs charly in the SAME
// image (issue #595):
//
//	charly: /usr/lib/charly/plugins/plugin-mcp exists in filesystem
//	error: failed to commit transaction (conflicting files)
//
// The collision cannot be cured by knowing WHICH plugins a package ships: that set varies per
// package build — the host package ships plugin-{candy,clean,doctor,feature,preempt,secrets,
// settings,tmux,udev,vm} while the build that failed also shipped plugin-mcp — so any in-tree
// list is stale by construction. /usr/local is the FHS location for locally-installed software,
// which no distro package owns, and that is what makes a bake collision-free for EVERY package
// build rather than for the ones we happen to know about.
//
// The image registers the directory through CHARLY_PLUGIN_DIR (see bakedPluginEnv), which
// charly's loader searches FIRST — ahead of the FHS dir — so every baked plugin still resolves in
// the deployed container while the package's own plugins stay reachable from the FHS path.
const bakedPluginImageDir = "/usr/local/lib/charly/plugins"

// bakedPluginEnv is the env var an image sets to register bakedPluginImageDir with the
// in-container charly. The name is duplicated (not imported) from charly/plugin_loader.go's
// bakedPluginDirs, which owns the search-order contract; a rename there must be mirrored here.
const bakedPluginEnv = "CHARLY_PLUGIN_DIR"

// bakedPluginFileName is the filename a baked plugin binary takes under bakedPluginDir. Keys by the
// plugin candy's LEAF name (the last path segment) — STABLE across how the candy is referenced.
func bakedPluginFileName(name string) string {
	return buildcache.SafePluginBinName(filepath.Base(name))
}

// FORMERLY HERE: a local `buildPluginBinary` copy, DELETED. It was a duplicate of
// charly/charly/plugin_loader.go's function of the same name and was NOT byte-identical to it —
// this file's comment claimed it was, and the claim was measurably false: the package-main copy
// grew CONTENT-STAMP REUSE (a stamp over the candy's source tree plus its transitive local
// `replace` targets, the toolchain and the build env, letting an unchanged plugin skip its
// rebuild) and this copy never did. Two live clusters for one concept, differing in exactly the
// freshness behaviour that matters, is the R2 duplication the one canonical home removes.
//
// EmitBakedPlugins now calls sdk/buildcache — the single implementation — so the bake path and the
// loader path share ONE key, ONE build and ONE set of freshness rules.

// EmitBakedPlugins bakes each composing candy's `bake_plugin:` out-of-tree plugin binaries into the
// FINAL image, staging under .build/<boxName>/.plugins/ and writing the COPY + chmod Containerfile
// fragment into b. Byte-identical logic to the former charly/generate.go
// (*Generator).emitBakedPlugins, adapted to run over (candies, buildDir) directly — no Generator,
// no host round-trip.
func EmitBakedPlugins(ctx context.Context, b *strings.Builder, buildDir, boxName string, candyOrder []string, candies map[string]CandyModel) error {
	baked := map[string]struct{}{}
	envEmitted := false
	for _, candyName := range candyOrder {
		layer := candies[candyName]
		if layer == nil || len(layer.GetBakePlugin()) == 0 {
			continue
		}
		for _, ref := range layer.GetBakePlugin() {
			key := ref.Bare()
			if _, done := baked[key]; done {
				continue
			}
			baked[key] = struct{}{}
			plugin := candies[key]
			if plugin == nil {
				return fmt.Errorf("candy %q: bake_plugin %q is not a known plugin candy (not in the scanned candy set)", candyName, key)
			}
			if plugin.GetSourceDir() == "" {
				return fmt.Errorf("candy %q: bake_plugin %q has no source dir to build from", candyName, key)
			}
			built, err := buildcache.Build(ctx, buildcache.Request{SrcDir: plugin.GetSourceDir(), Name: key})
			if err != nil {
				return fmt.Errorf("candy %q: bake_plugin %q: %w", candyName, key, err)
			}
			binPath := built.ViewPath
			binName := bakedPluginFileName(key)
			stageDir := filepath.Join(buildDir, boxName, ".plugins")
			if err := os.MkdirAll(stageDir, 0o755); err != nil {
				return fmt.Errorf("candy %q: bake_plugin %q: stage dir: %w", candyName, key, err)
			}
			if err := buildkit.CopyFileBytes(binPath, filepath.Join(stageDir, binName)); err != nil {
				return fmt.Errorf("candy %q: bake_plugin %q: stage binary: %w", candyName, key, err)
			}
			ctxRel := fmt.Sprintf(".build/%s/.plugins/%s", boxName, binName)
			dest := bakedPluginImageDir + "/" + binName
			if !envEmitted {
				fmt.Fprintf(b, "# Baked plugins go to %s, NOT into the distro package's own plugin directory\n", bakedPluginImageDir)
				fmt.Fprintf(b, "# (issue #595: a build step writing there makes the charly install refuse the image).\n")
				fmt.Fprintf(b, "# The in-container charly searches %s FIRST, so they still resolve at runtime.\n", bakedPluginEnv)
				fmt.Fprintf(b, "ENV %s=%s\n", bakedPluginEnv, bakedPluginImageDir)
				envEmitted = true
			}
			fmt.Fprintf(b, "# Bake plugin %q (required by %q) for in-container charly\n", key, candyName)
			fmt.Fprintf(b, "COPY %s %s\n", ctxRel, dest)
			fmt.Fprintf(b, "RUN chmod 0755 %s\n", dest)
			if plugin.IsPluginCandy() && len(plugin.GetPluginProviders()) > 0 {
				providers := plugin.GetPluginProviders()
				manifest := strings.Join(providers, "\n") + "\n"
				if err := os.WriteFile(filepath.Join(stageDir, binName+".providers"), []byte(manifest), 0o644); err != nil {
					return fmt.Errorf("candy %q: bake_plugin %q: stage manifest: %w", candyName, key, err)
				}
				fmt.Fprintf(b, "COPY %s.providers %s.providers\n", ctxRel, dest)
			}
			b.WriteString("\n")
		}
	}
	return nil
}
