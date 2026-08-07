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
// exactly like MaterializeSeams.DecodeEntity/BuildFleetEntity thread the registry-touching kind
// dispatch to a kind-blind mechanism. Neither callback's OWN body lives here; only the call shape
// does. Since K-wave 2 cone R1 those bodies live in candy/plugin-loader (refs_seams.go), reaching
// each peer over InvokeProvider — charly core, which used to supply them, is out of the fetch path
// entirely.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/opencharly/spec/refs"
	"github.com/opencharly/spec/spec"
)

// spec.RefsCollectSeams (loader_seam.go) carries the host-supplied callbacks this mechanism needs
// for everything registry-coupled (Downloader/MigrateCache/ResolveLocal/OverrideEnvValue) — this
// mechanism never touches the provider registry directly.

// autoMigratedRepos guards the remote-cache auto-migration against unbounded re-entry. A migration
// that re-enters LoadUnified would resolve @github refs and re-enter EnsureRepoDownloaded → the
// command:migrate Invoke. With a self- or mutual import (the main <-> cachyos cycle), and
// especially right after a LatestSchemaVersion bump (when EVERY cache reads as behind-head), that
// could recurse without bound. markRepoAutoMigrating returns true exactly once per cache path per
// process, so each cache is auto-migrated at most once and the cycle terminates — safe because the
// migration engine is idempotent, so a single pass per process is sufficient.
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

// normalizeOverrideRepoPath canonicalizes the LHS of a CHARLY_REPO_OVERRIDE pair to the repo-root
// form spec.ParseRemoteRef yields, so `opencharly/charly` and `github.com/opencharly/charly` both
// match (same auto-prefix rule as spec.NormalizeRepoSpec).
func normalizeOverrideRepoPath(rp string) string {
	rp = strings.TrimSpace(strings.TrimSuffix(rp, "/"))
	if i := strings.Index(rp, "/"); i > 0 && !strings.Contains(rp[:i], ".") {
		return "github.com/" + rp
	}
	return rp
}

// repoOverrideDir returns the configured local override directory for repoPath, or ("", false,
// nil) when none applies. envValue is the raw CHARLY_REPO_OVERRIDE value (a comma-separated list of
// `repoPath=localDir` pairs). A malformed entry, a missing/empty directory, or a non-directory
// target is a hard error — the override was set deliberately, so a typo must fail loud rather than
// silently fall through to a remote fetch.
func repoOverrideDir(repoPath, envValue string) (string, bool, error) {
	envValue = strings.TrimSpace(envValue)
	if envValue == "" {
		return "", false, nil
	}
	for pair := range strings.SplitSeq(envValue, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.LastIndex(pair, "=")
		if eq < 0 {
			return "", false, fmt.Errorf("CHARLY_REPO_OVERRIDE: malformed entry %q (want repoPath=localDir)", pair)
		}
		if normalizeOverrideRepoPath(pair[:eq]) != repoPath {
			continue
		}
		dir := strings.TrimSpace(pair[eq+1:])
		if dir == "" {
			return "", false, fmt.Errorf("CHARLY_REPO_OVERRIDE: empty directory for repo %q", repoPath)
		}
		if strings.HasPrefix(dir, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, dir[2:])
			}
		}
		info, err := os.Stat(dir)
		if err != nil {
			return "", false, fmt.Errorf("CHARLY_REPO_OVERRIDE: override dir for %q not accessible: %w", repoPath, err)
		}
		if !info.IsDir() {
			return "", false, fmt.Errorf("CHARLY_REPO_OVERRIDE: override for %q is not a directory: %s", repoPath, dir)
		}
		return dir, true, nil
	}
	return "", false, nil
}

// cacheBehindHead reports whether a cached repo still needs migration: its root config
// (charly.yml) is absent or carries a schema version older than HEAD. A cache already at HEAD with
// charly.yml returns false — the fast, silent path.
func cacheBehindHead(path string) bool {
	data, err := os.ReadFile(filepath.Join(path, spec.UnifiedFileName))
	if err != nil {
		return true // no charly.yml → never-migrated → migrate
	}
	cv, ok := spec.ParseCalVer(spec.FirstYAMLVersionLine(data))
	if !ok {
		return true
	}
	return cv.Less(spec.LatestSchemaCalVer())
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
	if dir, ok, err := repoOverrideDir(repoPath, seams.OverrideEnvValue); err != nil {
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
		path, err = seams.Downloader.Download(repoPath, version)
	}
	if err != nil {
		return "", err
	}
	// Migrate a fresh clone ALWAYS; migrate a cache HIT only when it is actually behind HEAD (an
	// older-schema cache). The chain is idempotent, but re-running it on every access of an
	// already-current cache is costly (re-parses every cached repo) and re-emits benign
	// "unknown field" warnings from very old transitive deps — so the already-current hit takes the
	// fast, silent path.
	if (!cached || cacheBehindHead(path)) && markRepoAutoMigrating(path) {
		if err := seams.MigrateCache(path); err != nil {
			return path, fmt.Errorf("auto-migrating remote cache %s: %w", path, err)
		}
	}
	return path, nil
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
	// Track resolved default branches per repo (to avoid duplicate git queries)
	defaultBranches := make(map[string]string)

	addRef := func(ref, source string) error {
		_ = source
		if !spec.IsRemoteCandyRefString(ref) {
			return nil
		}
		parsed := spec.ParseRemoteRef(ref)
		bareRef := spec.BareCandyRef(ref)
		version := parsed.Version
		if version == "" {
			// No version specified -- resolve to default branch
			if branch, ok := defaultBranches[parsed.RepoPath]; ok {
				version = branch
			} else {
				repoURL := refs.RepoGitURL(parsed.RepoPath)
				branch, err := refs.GitDefaultBranch(repoURL)
				if err != nil {
					return fmt.Errorf("%s: cannot resolve default branch for %s: %w", source, parsed.RepoPath, err)
				}
				version = branch
				defaultBranches[parsed.RepoPath] = branch
				fmt.Fprintf(os.Stderr, "Resolved @%s -> %s (default branch)\n", parsed.RepoPath, version)
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
	return result, nil
}
