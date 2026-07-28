package deploykit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// bake_plugins.go — the S0 baked-plugin BUILD-side seam (K3 build-tail move, coneB-buildtail):
// bakes each composing candy's `bake_plugin:` out-of-tree plugin binaries into the FINAL image at
// bakedPluginDir (/usr/lib/charly/plugins/), so a DEPLOYED container (which has neither the candy
// source nor a go toolchain) can run an external plugin its in-container charly needs at runtime.
//
// Relocated from charly/generate.go's (*Generator).emitBakedPlugins + charly/plugin_loader.go's
// buildPluginBinary cluster (RDD-spiked: buildPluginBinary is 100% pure os/exec — no host-only
// privilege, proven by the ALREADY-moved ensureCharlyBinaryFresh, which execs `go build` directly
// from candy/plugin-build with zero host round-trip). The former "bake-plugins" HostBuild round-trip
// (charly/host_build_bake_plugins.go, DELETED) is now unnecessary: EmitBakedPlugins runs this
// directly, wired by NewRenderGeneratorFromProject. buildPluginBinary/safePluginBinName/
// bakedPluginFileName/bakedPluginDir are duplicated from charly/plugin_loader.go (which ALSO still
// needs them for the runtime plugin LOADER's own bakedPluginBinary fallback — a genuinely different
// consumer, core-only) — the two modules cannot share private code (R3 cross-module precedent:
// candyByName, resolveUserContextPlugin, collectOverlayCandies all duplicate the same way).

// bakedPluginDir is the FHS system path a candy's `bake_plugin:` step copies a pre-built provider
// binary to at image-build time. Byte-identical to charly/plugin_loader.go's constant.
const bakedPluginDir = "/usr/lib/charly/plugins"

// safePluginBinName flattens a candy key (which may be an @github ref with slashes/colons) to a
// single filesystem-safe filename for the built binary.
func safePluginBinName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		default:
			return '_'
		}
	}, name)
}

// bakedPluginFileName is the filename a baked plugin binary takes under bakedPluginDir. Keys by the
// plugin candy's LEAF name (the last path segment) — STABLE across how the candy is referenced.
func bakedPluginFileName(name string) string {
	return safePluginBinName(filepath.Base(name))
}

// pluginBuildCacheDir is where built out-of-tree plugin binaries land.
func pluginBuildCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "charly", "plugins")
}

// pluginSourceTag returns a short, filesystem-safe digest of the plugin candy's resolved source
// directory, so the built binary's cache path is SCOPED BY SOURCE (the #76 root fix — see
// charly/plugin_loader.go's identical function for the full rationale).
func pluginSourceTag(srcDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(srcDir)))
	return hex.EncodeToString(sum[:8])
}

func pluginBuildVCSFlag(srcDir string, env []string) string {
	return pluginBuildVCSFlagForContext(srcDir, env, testing.Testing())
}

// pluginBuildVCSFlagForContext mirrors charly/plugin_loader.go's identical function — see there for
// the full rationale (charly#178 concurrent-worktree git-status race).
func pluginBuildVCSFlagForContext(srcDir string, env []string, isTestBinary bool) string {
	if isTestBinary {
		return "-buildvcs=false"
	}
	if pluginSourceHasGitRevision(srcDir, env) {
		return "-buildvcs=auto"
	}
	return "-buildvcs=false"
}

func pluginBuildEnv(base []string, srcDir string) []string {
	env := make([]string, 0, len(base)+4)
	for _, entry := range base {
		if strings.HasPrefix(entry, "GOWORK=") || strings.HasPrefix(entry, "GIT_") || strings.HasPrefix(entry, "PWD=") {
			continue
		}
		env = append(env, entry)
	}
	if absolute, err := filepath.Abs(srcDir); err == nil {
		srcDir = absolute
	}
	env = append(env, "GOWORK=off", "GIT_OPTIONAL_LOCKS=0", "PWD="+srcDir)
	return env
}

func pluginSourceHasGitRevision(srcDir string, env []string) bool {
	inside, ok := pluginGitProbe(srcDir, env, "rev-parse", "--is-inside-work-tree")
	if !ok || inside != "true" {
		return false
	}
	if _, ok := pluginGitProbe(srcDir, env, "status", "--porcelain"); !ok {
		return false
	}
	_, ok = pluginGitProbe(srcDir, env, "rev-parse", "--verify", "HEAD")
	return ok
}

func pluginGitProbe(srcDir string, env []string, args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Dir = srcDir
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// buildPluginBinary go-builds an out-of-tree plugin's provider binary (never in a venue — the
// caller owns the toolchain; the built binary is delivered into a venue by the in-venue transport).
// srcDir is the plugin candy's resolved dir, which is its own Go module. Byte-identical logic to
// charly/plugin_loader.go's function of the same name.
func buildPluginBinary(ctx context.Context, srcDir, name string) (string, error) {
	cacheDir := pluginBuildCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("plugin %q: build cache: %w", name, err)
	}
	bin := filepath.Join(cacheDir, safePluginBinName(name)+"-"+pluginSourceTag(srcDir))
	release, lockErr := kit.AcquireFileLock(bin+".lock", true)
	if lockErr != nil {
		return "", fmt.Errorf("plugin %q: acquire build lock: %w", name, lockErr)
	}
	defer func() { _ = release() }()
	binTmp := bin + ".build"
	target := "."
	if st, statErr := os.Stat(filepath.Join(srcDir, "cmd", "serve")); statErr == nil && st.IsDir() {
		target = "./cmd/serve"
	}
	buildEnv := pluginBuildEnv(os.Environ(), srcDir)
	buildVCS := pluginBuildVCSFlag(srcDir, buildEnv)
	cmd := exec.CommandContext(ctx, "go", "build", buildVCS, "-o", binTmp, target)
	cmd.Dir = srcDir
	cmd.Env = buildEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(binTmp)
		return "", fmt.Errorf("plugin %q: go build in %s: %w\n%s", name, srcDir, err, out)
	}
	if err := os.Rename(binTmp, bin); err != nil {
		_ = os.Remove(binTmp)
		return "", fmt.Errorf("plugin %q: publish build (rename %s -> %s): %w", name, binTmp, bin, err)
	}
	return bin, nil
}

// EmitBakedPlugins bakes each composing candy's `bake_plugin:` out-of-tree plugin binaries into the
// FINAL image, staging under .build/<boxName>/.plugins/ and writing the COPY + chmod Containerfile
// fragment into b. Byte-identical logic to the former charly/generate.go
// (*Generator).emitBakedPlugins, adapted to run over (candies, buildDir) directly — no Generator,
// no host round-trip.
func EmitBakedPlugins(ctx context.Context, b *strings.Builder, buildDir, boxName string, candyOrder []string, candies map[string]CandyModel) error {
	baked := map[string]struct{}{}
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
			binPath, err := buildPluginBinary(ctx, plugin.GetSourceDir(), key)
			if err != nil {
				return fmt.Errorf("candy %q: bake_plugin %q: %w", candyName, key, err)
			}
			binName := bakedPluginFileName(key)
			stageDir := filepath.Join(buildDir, boxName, ".plugins")
			if err := os.MkdirAll(stageDir, 0o755); err != nil {
				return fmt.Errorf("candy %q: bake_plugin %q: stage dir: %w", candyName, key, err)
			}
			if err := buildkit.CopyFileBytes(binPath, filepath.Join(stageDir, binName)); err != nil {
				return fmt.Errorf("candy %q: bake_plugin %q: stage binary: %w", candyName, key, err)
			}
			ctxRel := fmt.Sprintf(".build/%s/.plugins/%s", boxName, binName)
			dest := bakedPluginDir + "/" + binName
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
