package loaderkit

// canonical_ref.go — CanonicalRef, the import-ref resolution mechanism the whole-project WALK
// reaches through spec.WalkSeams.ResolveRef.
//
// It resolves one `import:` ref — a local path, or `@host/org/repo[/sub/path]:version` — to a
// concrete on-disk path AND a stable cache key. The key is what dedups identical refs across a whole
// load, so a diamond of namespaced imports (or the intentional main<->cachyos cycle) resolves exactly
// once; walk.go consults it at both its import-queue and namespace-mount sites.
//
// This lived in charly core (unified.go's canonicalRef) purely because the walk's ResolveRef seam was
// wired there. That was call-not-define residue: the body is kind-blind ref vocabulary
// (spec.ParseRemoteRef) plus a default-branch resolve plus the one fetch EnsureRepoDownloaded
// already owns in this package — no registry, no host state. It sits beside that fetch now, and the
// host's seam wiring is a one-line forward through the loader plugin (K-wave 2 cone R1 unit 3),
// exactly like ResolveProjectRepo before it.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencharly/spec/refs"
	"github.com/opencharly/spec/spec"
)

// CanonicalRef resolves ref (relative to baseDir) to its dedup key and its on-disk path. A remote
// ref is downloaded into the shared repo cache first (and auto-migrated) via EnsureRepoDownloaded;
// an unpinned remote ref resolves to the repo's default branch. A local ref resolves to its absolute
// path, which is both key and path.
func CanonicalRef(ref, baseDir string, seams spec.RefsCollectSeams) (key, path string, err error) {
	if strings.HasPrefix(ref, "@") {
		parsed := spec.ParseRemoteRef(ref)
		version := parsed.Version
		if version == "" {
			branch, e := refs.GitDefaultBranch(refs.RepoGitURL(parsed.RepoPath))
			if e != nil {
				return "", "", fmt.Errorf("resolving default branch for %s: %w", parsed.RepoPath, e)
			}
			version = branch
		}
		cachePath, e := EnsureRepoDownloaded(parsed.RepoPath, version, seams)
		if e != nil {
			return "", "", fmt.Errorf("downloading remote ref %q: %w", ref, e)
		}
		return parsed.RepoPath + "@" + version + "/" + parsed.SubPath,
			filepath.Join(cachePath, parsed.SubPath), nil
	}
	p := ref
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, ref)
	}
	abs, e := filepath.Abs(p)
	if e != nil {
		return "", "", fmt.Errorf("resolving %s: %w", ref, e)
	}
	return abs, abs, nil
}
