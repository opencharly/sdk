package deploykit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// header_copy_remote.go — rewriteHeaderCopyForRemote, promoted from charly/generate.go's
// (*Generator).rewriteHeaderCopyForRemote + its remoteBuildConfigCacheRoot/
// materializeBuildConfigAsset helpers (K3 render-seam production move). The ONLY charly-core
// state these needed was already available from the resolved-project envelope's CandyModel view
// (Remote/SourceDir/SubPathPrefix/Name, all exposed by the CandyModel interface) plus the render
// dir + build dir the plugin already holds — so this needs no host callback at all.

// remoteBuildConfigCacheRoot finds the shared repo@version cache root a remote candy's build.yml
// was read from, by stripping the candy subpath off any remote candy's cached SourceDir (every
// remote candy + the remote build.yml share one repo@version cache).
func remoteBuildConfigCacheRoot(candies map[string]CandyModel) string {
	for _, l := range candies {
		if l == nil || !l.GetRemote() || l.GetSourceDir() == "" {
			continue
		}
		suffix := filepath.Join(l.GetSubPathPrefix(), l.GetName())
		if trimmed, ok := strings.CutSuffix(l.GetSourceDir(), suffix); ok {
			return strings.TrimRight(trimmed, string(filepath.Separator))
		}
	}
	return ""
}

// materializeBuildConfigAsset ensures a build-config asset file (referenced by a remotely-included
// build.yml — e.g. the init header_file) is available in the build context. If the project ships
// the file locally (local build.yml), relPath is returned unchanged. Otherwise the file is copied
// from the remote build-config cache into buildDir/_buildconfig/<relPath> (gitignored, like
// .build/_candy/) and the build-root-relative path is returned for use as a COPY source.
func materializeBuildConfigAsset(candies map[string]CandyModel, dir, buildDir, relPath string) (string, error) {
	if relPath == "" {
		return relPath, nil
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err == nil {
		return relPath, nil // local build-config ships the asset; COPY works as-is
	}
	root := remoteBuildConfigCacheRoot(candies)
	if root == "" {
		return relPath, nil // no remote source to pull from; leave as authored
	}
	srcAbs := filepath.Join(root, relPath)
	if _, err := os.Stat(srcAbs); err != nil {
		return relPath, nil // not in the remote cache either; leave as authored
	}
	destAbs := filepath.Join(buildDir, "_buildconfig", relPath)
	if err := os.MkdirAll(filepath.Dir(destAbs), 0755); err != nil {
		return relPath, err
	}
	if out, err := exec.Command("cp", "-a", srcAbs, destAbs).CombinedOutput(); err != nil {
		return relPath, fmt.Errorf("materializing build-config asset %s: %s: %w", relPath, string(out), err)
	}
	return filepath.ToSlash(filepath.Join(".build", "_buildconfig", relPath)), nil
}

// rewriteHeaderCopyForRemote rewrites a `COPY <src> <dst>` header directive so its source points
// at a materialized build-config asset when the original src isn't in the local build context.
// Plain 3-token COPY only; anything else passes through.
func rewriteHeaderCopyForRemote(candies map[string]CandyModel, dir, buildDir, headerCopy string) (string, error) {
	fields := strings.Fields(headerCopy)
	if len(fields) != 3 || fields[0] != "COPY" {
		return headerCopy, nil
	}
	newSrc, err := materializeBuildConfigAsset(candies, dir, buildDir, fields[1])
	if err != nil {
		return headerCopy, err
	}
	if newSrc == fields[1] {
		return headerCopy, nil
	}
	return fmt.Sprintf("COPY %s %s", newSrc, fields[2]), nil
}
