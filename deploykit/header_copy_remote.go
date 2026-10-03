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
//
// When NEITHER location carries the asset the call FAILS. `dir` is the build context the emitted
// `COPY <src>` resolves against — the Containerfile is written to buildDir/<box>/Containerfile and
// the build runs with `dir` as its context, which is exactly why the materialized branch returns a
// `.build/_buildconfig/...` path relative to `dir` rather than to buildDir. Both probes below
// therefore test the very path a `COPY <relPath>` would resolve to, so once both have missed the
// directive cannot do anything but fail inside the engine — with a
// `copier: stat: "<relPath>": no such file or directory` that names neither the candy nor the
// cache. Failing here instead can never reject a build that would otherwise have succeeded (the
// fallthrough was reachable ONLY when the COPY was already doomed); it converts an opaque engine
// error into a generate-time one that reports the asset and every root that was searched.
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
	roots := remoteBuildConfigCacheRoots(candies)
	for _, root := range roots {
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
	searched := "no remote candy is in the resolved set"
	if len(roots) > 0 {
		searched = strings.Join(roots, ", ")
	}
	return relPath, fmt.Errorf(
		"build-config asset %q cannot be resolved: it is not in the project tree (%s), and not in any "+
			"resolved candy's repo@version cache root (%s). The COPY this renders would name a source "+
			"that provably does not exist, so the image build would fail inside the engine with an "+
			"opaque \"no such file or directory\" naming neither the candy nor the cache. Check that "+
			"the candy whose build.yml references %q is in the resolved set (a candy that was never "+
			"fetched leaves no cache root to search) and that the path is spelled as it is in that "+
			"candy's repo",
		relPath, filepath.Join(dir, relPath), searched, relPath)
}

// rewriteHeaderCopyForRemote rewrites a `COPY <src> <dst>` header directive so its source points at
// the materialized build-config asset. A line that is not a 3-field COPY is passed through
// as-authored; a COPY whose source resolves in neither the project tree nor any remote cache root
// is an ERROR (see materializeBuildConfigAsset), never a silently emitted directive that cannot
// build.
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
