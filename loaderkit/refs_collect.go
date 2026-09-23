package loaderkit

// refs_collect.go — the remote-repo fetch ORCHESTRATION + candy-ref collection mechanism (K1 unit
// 4, relocated from charly/refs.go): EnsureRepoDownloaded (local-override short-circuit, cache-hit
// check, cache-miss dispatch through the RefsDownloader backend, post-fetch schema auto-migration)
// and CollectRemoteRefsOpts (the depth-first base/builder/candy-ref graph walk over a
// *spec.Config). Both operate on spec.Config/spec.CandyReader/spec.ResolveOpts directly — the
// former in-core `Config = spec.Config` alias (charly/config.go) is gone (W0 dissolved it), and
// charly/refs.go's thin wrapper functions now take `*spec.Config` directly, so this relocation adds
// no new dependency, just repoints through the ALREADY spec-legal underlying type.
//
// sdk/kit/refs_downloader.go's own doc comment ("the host keeps the fetch ORCHESTRATION... the
// boundary is the backend that turns a (repoPath, version) into a populated local cache tree") is
// P7-era prose predating this relocation — that boundary was correct for P7's own time (plugins
// could not yet touch host state), but the v2 end-state ("core does not parse config, resolve,
// build, deploy, or check") supersedes it exactly the way K1 unit 1 already superseded
// materialize.go's own former "stays core, clause M" self-classification (the canonical
// boundary-law precedent for this exact situation). The kit comment is fixed in the same cutover.
//
// The TWO genuinely registry-coupled calls this mechanism used to make directly — the local-template
// substrate-plugin resolve and the command:migrate dispatch — thread in as callback parameters,
// exactly like MaterializeSeams.DecodeEntity/BuildDeployEntity thread the registry-touching kind
// dispatch to a kind-blind mechanism. Neither callback's OWN body lives here; only the call shape
// does. Since K-wave 2 cone R1 those bodies live in candy/plugin-loader (refs_seams.go), reaching
// each peer over InvokeProvider — charly core, which used to supply them, is out of the fetch path
// entirely.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/opencharly/spec/calver"
	"github.com/opencharly/spec/lock"
	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/refs"
	"github.com/opencharly/spec/spec"
)

// spec.RefsCollectSeams (loader_seam.go) carries the host-supplied callbacks this mechanism needs
// for everything registry-coupled (Downloader/MigrateCache/ResolveLocal/OverrideEnvValue) — this
// mechanism never touches the provider registry directly.

// autoMigratedRepos guards the DERIVED-VIEW build against unbounded re-entry. Building a view runs
// the migration, which re-enters LoadUnified, which resolves @github refs and re-enters
// EnsureRepoDownloaded → DeriveRepoView for the SAME view (a self- or mutual import cycle such as
// main <-> cachyos). markRepoAutoMigrating returns true exactly once per view path per process, so
// each view is derived at most once and the cycle terminates — safe because the migration engine is
// idempotent, so a single pass per process is sufficient.
var (
	autoMigratedRepos   = map[string]bool{}
	autoMigratedReposMu sync.Mutex
)

func markRepoAutoMigrating(path string) bool {
	autoMigratedReposMu.Lock()
	defer autoMigratedReposMu.Unlock()
	if autoMigratedRepos[path] {
		return false
	}
	autoMigratedRepos[path] = true
	return true
}

// RepoOverrideDir returns the configured local override directory for repoPath, or ("", false,
// nil) when none applies. The parse itself lives in spec/proc (next to RepoOverrideEnv) so the
// fetch LEAF (spec/refs.DownloadRepo) can share the SAME one — R3, one implementation per
// behavior. This wrapper keeps the sdk-side seam (spec.ProjectLoader.RepoOverrideDir / charly
// core's provenance logging) on that single source.
func RepoOverrideDir(repoPath, envValue string) (string, bool, error) {
	return proc.RepoOverrideDir(repoPath, envValue)
}

// cacheBehindHead reports whether a tree still needs migration: its root config
// (charly.yml) is absent or carries a schema version older than HEAD. A tree already at HEAD with
// charly.yml returns false — the fast, silent path.
func cacheBehindHead(path string) bool {
	data, err := os.ReadFile(filepath.Join(path, spec.UnifiedFileName))
	if err != nil {
		return true // no charly.yml → never-migrated → migrate
	}
	cv, ok := calver.ParseCalVer(spec.FirstYAMLVersionLine(data))
	if !ok {
		return true
	}
	return cv.Less(calver.LatestSchemaCalVer())
}

// derivedViewPath is the schema-keyed derived-view directory for a pristine cache
// export: <cachePath>.view.<headSchema>. The view holds the SAME tree migrated to
// HEAD schema. It is keyed by the head schema CalVer only, because the pristine
// cache path is already keyed by the resolved commit (<repo>@<ref>, and a mutable
// ref is RE-FETCHED to a fresh export when it moves) — so a schema bump is the only
// thing that can make an existing view stale, and it yields a new path.
func derivedViewPath(cachePath string) string {
	return cachePath + ".view." + calver.LatestSchemaCalVer().String()
}

// viewMarkerName marks a fully-built derived view. Its presence (after the atomic
// rename of the completed copy) is the completeness proof: a half-built view has no
// marker and is rebuilt, so a crash mid-migrate never publishes a torn view.
const viewMarkerName = ".charly-view-ok"

// DeriveRepoView returns a directory holding the repo's project files migrated to the
// HEAD schema, WITHOUT ever mutating the pristine cache export. The pristine export
// is read-only shared state across binaries of different schema versions; the OLD
// implementation rewrote it in place (auto-migrating charly.yml to the CONSUMER's
// schema CalVer), so a newer binary poisoned the cache for every older consumer —
// the exact defect that failed check-substrate's deploy-add with "config schema
// <newer> is newer than this charly supports".
//
// Behavior:
//   - a tree already at HEAD is returned AS-IS (no copy, the fast path);
//   - otherwise a schema-keyed view is materialized once (copy + migrate under a
//     per-view flock, published by atomic rename) and reused on later accesses.
//
// The migrate callback is the registry-coupled command:migrate leg (seams.MigrateCache);
// passing it in keeps this mechanism kind-blind and unit-testable.
func DeriveRepoView(cachePath string, migrate func(path string) error) (string, error) {
	if !cacheBehindHead(cachePath) {
		return cachePath, nil
	}
	viewPath := derivedViewPath(cachePath)
	if _, err := os.Stat(filepath.Join(viewPath, viewMarkerName)); err == nil {
		return viewPath, nil
	}
	// Re-entry guard: a migration re-enters LoadUnified, which resolves @github
	// refs and re-enters EnsureRepoDownloaded → DeriveRepoView for the SAME view (a
	// self/mutual import cycle such as main <-> cachyos). Without this the second
	// acquire of the view flock would deadlock (flock is per open-file-description,
	// so a re-acquire in the same process blocks). Admit each view once per process;
	// a re-entrant call uses whatever the in-flight build will publish, preferring an
	// already-published view and otherwise the pristine tree. Idempotent migration
	// makes one pass sufficient.
	if !markRepoAutoMigrating(viewPath) {
		if _, err := os.Stat(filepath.Join(viewPath, viewMarkerName)); err == nil {
			return viewPath, nil
		}
		return cachePath, nil
	}
	release, err := lock.AcquireFileLock(viewPath+".lock", true)
	if err != nil {
		// NEVER fall back to migrating cachePath in place: the pristine export is
		// read-only shared state across binaries of different schema versions, and
		// rewriting it is the exact defect this function exists to remove. A lock/IO
		// failure is a hard load failure (the caller propagates it) — the pristine
		// tree stays untouched, so a retry derives cleanly.
		return "", fmt.Errorf("deriving schema view of %s: acquiring view lock: %w", cachePath, err)
	}
	defer func() { _ = release() }()
	// Re-check under the lock: a concurrent first-misser may have built the view.
	if _, err := os.Stat(filepath.Join(viewPath, viewMarkerName)); err == nil {
		return viewPath, nil
	}
	tmpPath := viewPath + ".tmp"
	_ = os.RemoveAll(tmpPath)
	if err := copyTree(cachePath, tmpPath); err != nil {
		_ = os.RemoveAll(tmpPath)
		return "", fmt.Errorf("deriving schema view of %s: %w", cachePath, err)
	}
	if err := migrate(tmpPath); err != nil {
		_ = os.RemoveAll(tmpPath)
		return "", fmt.Errorf("migrating derived view of %s: %w", cachePath, err)
	}
	if err := os.WriteFile(filepath.Join(tmpPath, viewMarkerName), []byte(calver.LatestSchemaCalVer().String()+"\n"), 0o644); err != nil {
		_ = os.RemoveAll(tmpPath)
		return "", err
	}
	_ = os.RemoveAll(viewPath)
	if err := os.Rename(tmpPath, viewPath); err != nil {
		_ = os.RemoveAll(tmpPath)
		return "", fmt.Errorf("publishing derived view of %s: %w", cachePath, err)
	}
	return viewPath, nil
}

// copyTree copies a directory tree (files, modes, symlink targets). It is the derive
// step's pure half — no git, no network.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			return os.Symlink(link, target)
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// EnsureRepoDownloaded downloads the repo if not already cached. Returns the cache path. The cache
// is auto-migrated to the latest schema CalVer via seams.MigrateCache on EVERY access — cache HIT
// and fresh clone alike. Re-migrating a cache hit is required (and safe, the chain being
// idempotent): a cache populated by an OLDER binary — or relocated from a prior cache directory
// across a schema bump (an older-schema cache) — so the current binary would otherwise fail to find
// charly.yml. An already-current cache is a no-op.
func EnsureRepoDownloaded(repoPath, version string, seams spec.RefsCollectSeams) (string, error) {
	// RDD local-override (CHARLY_REPO_OVERRIDE): resolve a remote repo ref to a local working tree
	// instead of fetching, so an uncommitted candy/charly.yml change can be built + evaluated by
	// any consumer before it is pushed. The override is the dev's LIVE tree — it is used verbatim
	// and NEVER migrated (migration would mutate the working tree); the dev keeps it
	// schema-current themselves.
	if dir, ok, err := RepoOverrideDir(repoPath, seams.OverrideEnvValue); err != nil {
		return "", err
	} else if ok {
		return dir, nil
	}
	cached, err := refs.IsRepoCached(repoPath, version)
	if err != nil {
		return "", err
	}
	var path string
	if cached && !refs.IsMutableRef(version) {
		path, err = refs.RepoCachePath(repoPath, version)
	} else {
		// The cache-miss DOWNLOAD dispatches through the registered refs backend (P7): the
		// compiled-in candy/plugin-refs (git) by default, swappable for an OCI/S3 plugin. A
		// MUTABLE ref (a branch such as main, or the unversioned default branch) always delegates:
		// the downloader re-resolves the ref's current commit and refreshes a stale export (the
		// refs.DownloadRepo provenance check) — a plain cache hit would freeze the branch at its
		// first-download content forever (the pre-#146 @main protocol skew). Immutable coordinates
		// (tags, SHAs) keep the offline cache hit.
		//
		// The centralized git layer CACHES the mutable-ref download with a short TTL (5m): a
		// mutable branch can move, but not on every invocation — the status fan-out resolving
		// the envelope multiple times pays the download once (issue #423, #208).
		path, err = gitClient().Download(repoPath, version, seams.Downloader.Download)
	}
	if err != nil {
		return "", err
	}
	// DERIVE the head-schema view instead of MUTATING the pristine cache. Sharing one mutable
	// export across binaries of different schema versions is what let a newer binary poison the
	// cache for older consumers (the check-substrate deploy-add failure). The pristine export
	// stays exactly as fetched; each schema version reads its own derived view.
	return DeriveRepoView(path, seams.MigrateCache)
}

// CollectRemoteRefs is the default-opts wrapper (enabled images only) around CollectRemoteRefsOpts.
// The overwhelming majority of call sites want enabled-only collection, so they keep this
// three-arg form.
func CollectRemoteRefs(cfg *spec.Config, layers map[string]spec.CandyReader, seams spec.RefsCollectSeams) ([]spec.RemoteDownload, error) {
	return CollectRemoteRefsOpts(cfg, layers, spec.ResolveOpts{}, seams)
}

// CollectRemoteRefsOpts collects all unique remote refs from charly.yml candy lists and candy
// manifest depends/candy fields. Different candies from the same repo can use different versions.
// Only the same bare ref at conflicting versions is an error. Returns a list of
// spec.RemoteDownload grouped by (repoPath, version).
//
// opts gates the disabled-image walk: a disabled image's candy refs are collected when
// opts.ShouldIncludeDisabled(name) is true (i.e. a `--include-disabled <name>` build). This keeps
// the remote-ref FETCH set in lockstep with the RESOLVE set walked by ResolveAllBox /
// GlobalCandyOrder — the same shouldIncludeDisabled predicate gates both. Without it, a disabled
// named image lands in the build working set but its remote candies are never fetched/registered,
// surfacing as "unknown layer" while computing global candy order.
//
//nolint:gocyclo // depth-first graph walker over base/candy/builder edges; nested loops are essential to the traversal
func CollectRemoteRefsOpts(cfg *spec.Config, layers map[string]spec.CandyReader, opts spec.ResolveOpts, seams spec.RefsCollectSeams) ([]spec.RemoteDownload, error) {
	// Collect EVERY distinct (repo, git-tag) a ref is referenced at. The git tag is only the FETCH
	// coordinate — per-entity-version arbitration (and any warning) happens AFTER fetch in
	// ScanAllCandyWithConfigOpts, so a re-tag of an unchanged candy no longer warns here. `source`
	// is unused now (kept for call-site stability + future diagnostics).
	type repoVer struct{ repo, ver string }
	pairs := make(map[repoVer]map[string]bool) // (repo, git-tag) -> set of bare refs
	// Track resolved latest tags per repo (to avoid duplicate git queries)
	latestTags := make(map[string]string)

	addRef := func(ref, source string) error {
		_ = source
		if !spec.IsRemoteCandyRefString(ref) {
			return nil
		}
		parsed := spec.ParseRemoteRef(ref)
		bareRef := spec.BareCandyRef(ref)
		version := parsed.Version
		if version == "" {
			// No version specified -- resolve to the LATEST TAG (the candy de-submodule cutover,
			// Phase 4). A version-less remote ref (e.g. a builder plugin connected by word ref,
			// where the caller knows the repo but not the tag) previously fell back to the
			// MUTABLE default branch — a freshness-checked ref that hangs in a network-bound
			// CI/container and can advance under the resolver. The newest tag is immutable and
			// deterministic; a repo with no tags errors loudly rather than silently pinning a
			// branch.
			if tag, ok := latestTags[parsed.RepoPath]; ok {
				version = tag
			} else {
				repoURL := refs.RepoGitURL(parsed.RepoPath)
				resolveTag := seams.LatestTag
				if resolveTag == nil {
					resolveTag = gitClient().LatestTag // the CACHED latest-tag (1h TTL, disk-persisted, cross-process) — the raw GitLatestTag was the per-process ls-remote fanout (measured: 90-94 concurrent ls-remote -> throttling)
				}
				tag, err := resolveTag(repoURL)
				if err != nil {
					return fmt.Errorf("%s: cannot resolve latest tag for %s: %w", source, parsed.RepoPath, err)
				}
				version = tag
				latestTags[parsed.RepoPath] = tag
				fmt.Fprintf(os.Stderr, "Resolved @%s -> %s (latest tag)\n", parsed.RepoPath, version)
			}
		}
		key := repoVer{parsed.RepoPath, version}
		if pairs[key] == nil {
			pairs[key] = make(map[string]bool)
		}
		pairs[key][bareRef] = true
		return nil
	}

	// format_config: has been removed. Remote build-config refs now live in charly.yml's
	// `includes:` mechanism.

	// Collect candy refs from the ROOT project's own build/deploy targets (every enabled image +
	// every kind:local template), then follow base/builder edges into imported namespaces,
	// collecting ONLY the namespaced images actually reachable as a base or builder. A namespace is
	// imported to provide bases/builders; its UNREFERENCED images and its kind:local templates
	// (which can never be a base/builder of the importing project) are not build inputs here and
	// must not be collected. Over-collecting them pulled unrelated candies pinned at a different
	// ecosystem tag, which the one-candy-one-version invariant (tracker) then correctly — but
	// spuriously — rejected. The per-(Config,name) `collected` set also breaks the main<->cachyos
	// cycle.
	collected := map[*spec.Config]map[string]bool{}
	var collectBox func(c *spec.Config, name string) error
	collectBox = func(c *spec.Config, name string) error {
		seen := collected[c]
		if seen == nil {
			seen = map[string]bool{}
			collected[c] = seen
		}
		if seen[name] {
			return nil
		}
		seen[name] = true
		img, ok := c.BoxConfig(name)
		if !ok {
			return nil // external OCI base or unknown name — no candies to collect
		}
		for _, candyRef := range img.Candy {
			if err := addRef(candyRef, fmt.Sprintf("image %s", name)); err != nil {
				return err
			}
		}
		// Follow the base edge, plus builder edges when this image actually builds (a candyless
		// base needs no builder). A namespaced builder (e.g. charly.fedora-builder) is BUILT as an
		// intermediate in the consumer's graph, so its candies (rpmfusion, yay, …) must be fetched
		// here — dropping the builder edge under-collects them ("unknown layer"). The builder edge
		// follows the EFFECTIVE builder (effectiveBuilderForBox → the canonical
		// resolveEffectiveBuilder), NOT the raw per-image img.Builder: an image whose builder comes
		// from defaults.builder / the distro-keyed default (e.g. bazzite/aurora ->
		// charly.fedora-builder, with no per-image builder: block) has an EMPTY raw img.Builder, so
		// reading it skipped the builder edge and under-collected its candies — the exact
		// fetch/resolve lockstep break this walk exists to prevent. Qualified refs descend into the
		// imported namespace; bare refs resolve within c; an external-URL/unknown base resolves to
		// ok=false and is skipped.
		edges := []string{}
		if img.Base != "" {
			edges = append(edges, img.Base)
		}
		if len(img.Candy) > 0 {
			edges = append(edges, spec.EffectiveBuilderForBox(c, name, img).AllBuilder()...)
		}
		for _, ref := range edges {
			if _, tc, ok := c.ResolveBoxRef(ref); ok {
				if err := collectBox(tc, spec.LeafName(ref)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if cfg != nil {
		for _, imgName := range cfg.AllBoxNames() {
			img, _ := cfg.BoxConfig(imgName)
			if !img.IsEnabled() && !opts.ShouldIncludeDisabled(imgName) {
				continue
			}
			if err := collectBox(cfg, imgName); err != nil {
				return nil, err
			}
		}
		// Pull in any explicitly-requested namespace-qualified targets too (task #17 fix — mirrors
		// buildkit.ResolveAllBox's own opts.RequestedBoxes handling for the RESOLVE half): the walk
		// above only follows base/builder edges from ROOT-owned images, so an on-demand
		// namespace-qualified target (`charly box generate fedora.check-pod`) that is not itself a
		// base/builder of any root image is otherwise never visited — its own remote candy refs
		// (including a back-ref to this very repo) are then silently never collected, and the later
		// candy-order resolve fails "unknown candy" for a ref the fetch step skipped. A bare
		// (non-qualified) requested name is already covered by the AllBoxNames() loop above.
		for _, name := range opts.RequestedBoxes {
			if _, _, qualified := spec.SplitNamespaceRef(name); !qualified {
				continue
			}
			if _, tc, ok := cfg.ResolveBoxRef(name); ok {
				if err := collectBox(tc, spec.LeafName(name)); err != nil {
					return nil, err
				}
			}
		}
		for tplName, body := range cfg.Local {
			r, rerr := seams.ResolveLocal(body)
			if rerr != nil || r == nil {
				continue
			}
			for _, candyRef := range r.Candy {
				if err := addRef(candyRef, fmt.Sprintf("kind:local %s", tplName)); err != nil {
					return nil, err
				}
			}
		}
	}

	// Scan the candy manifest require: and candy: fields
	for candyName, layer := range layers {
		for _, dep := range layer.GetRequire() {
			if err := addRef(dep.Raw, fmt.Sprintf("layer %s require", candyName)); err != nil {
				return nil, err
			}
		}
		for _, ref := range layer.GetIncludedCandy() {
			if err := addRef(ref.Raw, fmt.Sprintf("layer %s layer", candyName)); err != nil {
				return nil, err
			}
		}
	}

	// A deploy's add_candy: candies (opts.ExtraCandyRefs) are NOT reachable from the image-closure
	// walk above (add_candy is not a base/builder/require edge), so a bed that add_candy's a
	// host-side PLUGIN candy must collect them here — else the plugin never enters the scan and
	// loadProjectPlugins can't build it. A local ref is a no-op (addRef gates on
	// IsRemoteCandyRef; ScanCandy already has it); a remote ref joins the same fetch +
	// per-entity-version arbitration as any other.
	for _, ref := range opts.ExtraCandyRefs {
		if err := addRef(ref, "deploy add_candy"); err != nil {
			return nil, err
		}
	}

	// Emit one spec.RemoteDownload per distinct (repo, git-tag). A bare ref pinned at two git tags
	// yields two downloads (both fetched); the post-fetch arbitration keeps one materialization per
	// bare ref.
	var result []spec.RemoteDownload
	for key, refSet := range pairs {
		refList := make([]string, 0, len(refSet))
		for ref := range refSet {
			refList = append(refList, ref)
		}
		result = append(result, spec.RemoteDownload{
			RepoPath: key.repo,
			Version:  key.ver,
			Refs:     refList,
		})
	}
	// FIRST-STARTUP WARM-UP: when the git cache is cold, prefetch the version-less
	// refs' latest tags + default branches with a clear message so the user knows
	// charly is fetching git metadata (issue #423, #208). The centralized git layer
	// caches the results, so subsequent invocations are fast.
	if len(pairs) > 0 {
		var repos []string
		seen := map[string]bool{}
		for key := range pairs {
			if !seen[key.repo] {
				seen[key.repo] = true
				repos = append(repos, refs.RepoGitURL(key.repo))
			}
		}
		gitClient().WarmUp(repos, os.Stderr)
	}
	return result, nil
}
