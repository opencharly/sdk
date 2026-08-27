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

// remoteBuildConfigCacheRoots returns the DISTINCT repo@version cache roots every remote candy's
// build.yml was read from, by stripping each candy's subpath off its cached SourceDir. Before the
// candy de-submodule cutover every remote candy + the remote build.yml shared ONE repo@version
// cache (the charly repo); the cutover moved each candy to its own standalone repo, so the cache
// roots are now per-repo and the build-config asset (e.g. the supervisord init header_file) may
// live in ANY of them. Deduplicated so a repo with several candies yields one root.
func remoteBuildConfigCacheRoots(candies map[string]CandyModel) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range candies {
		if l == nil || !l.GetRemote() || l.GetSourceDir() == "" {
			continue
		}
		var root string
		// A ROOT-LEVEL standalone candy (the de-submodule cutover's shape: the manifest lives at
		// the repo root, ref == repoPath) has an empty SubPathPrefix and its SourceDir IS the
		// repo@version cache root itself — no suffix to strip. A subpath candy (old
		// candy/<name> inside the charly repo, or any future multi-candy repo) carries
		// SubPathPrefix like "candy/"; strip it to reach the shared cache root.
		if l.GetSubPathPrefix() == "" {
			root = l.GetSourceDir()
		} else {
			suffix := filepath.Join(l.GetSubPathPrefix(), l.GetName())
			trimmed, ok := strings.CutSuffix(l.GetSourceDir(), suffix)
			if !ok {
				continue
			}
			root = strings.TrimRight(trimmed, string(filepath.Separator))
		}
		if root != "" && !seen[root] {
			seen[root] = true
			out = append(out, root)
		}
	}
	return out
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
	// Search EVERY distinct repo@version cache root — after the candy de-submodule cutover each
	// remote candy lives in its own standalone repo, so the build-config asset (e.g. the init
	// header_file) may live in any candy's cache root, not just the first.
	for _, root := range remoteBuildConfigCacheRoots(candies) {
		srcAbs := filepath.Join(root, relPath)
		if _, err := os.Stat(srcAbs); err != nil {
			continue
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
	return relPath, nil // not in any remote cache root; leave as authored
}

// rewriteHeaderCopyForRemote rewrites a `COPY <src> <dst>` header directive so its source points
// at the materialized build-config asset (or stays as-authored when no remote source is found).
func rewriteHeaderCopyForRemote(candies map[string]CandyModel, dir, buildDir, headerCopy string) (string, error) {
	fields := strings.Fields(headerCopy)
	if len(fields) != 3 || fields[0] != "COPY" {
		return headerCopy, nil
	}
	newSrc, err := materializeBuildConfigAsset(candies, dir, buildDir, fields[1])
	if err != nil {
		return headerCopy, err
	}
	return "COPY " + newSrc + " " + fields[2], nil
}
