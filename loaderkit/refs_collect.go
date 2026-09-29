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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/opencharly/spec/lock"
	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/refs"
	"github.com/opencharly/spec/spec"
)

// spec.RefsCollectSeams (loader_seam.go) carries the host-supplied callbacks this mechanism needs
// for everything registry-coupled (Downloader/MigrateCache/ResolveLocal/OverrideEnvValue) — this
// mechanism never touches the provider registry directly.

// RepoOverrideDir returns the configured local override directory for repoPath, or ("", false,
// nil) when none applies. The parse itself lives in spec/proc (next to RepoOverrideEnv) so the
// fetch LEAF (spec/refs.DownloadRepo) can share the SAME one — R3, one implementation per
// behavior. This wrapper keeps the sdk-side seam (spec.ProjectLoader.RepoOverrideDir / charly
// core's provenance logging) on that single source.
func RepoOverrideDir(repoPath, envValue string) (string, bool, error) {
	return proc.RepoOverrideDir(repoPath, envValue)
}

// autoMigratedRepos guards the DERIVED-VIEW build against unbounded re-entry. Building a
// view runs the reshape, which re-enters LoadUnified, which resolves @github refs and
// re-enters EnsureRepoDownloaded → DeriveRepoView for the SAME view (a self- or mutual
// import cycle such as main <-> cachyos). markRepoAutoMigrating returns true exactly once
// per view path per process, so each view is derived at most once and the cycle terminates
// — safe because the reshape engine is idempotent, so a single pass per process suffices.
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

// reshapeViewIdentity is the CONTENT-ADDRESSED identity of the reshape a derived view
// represents: the sha256 (first 16 hex chars) of the schema module version + the loader
// module version. A schema change (spec) or a loader/reshape change (sdk) yields a new
// identity → a new view path, so a stale view is never reused. This replaces the removed
// schema CalVer key: there is no `version:` in charly.yml any more — only the CalVer GIT
// tags (used for ref resolution) — so the reshape key is a content sha, not a version.
func reshapeViewIdentity() string {
	return reshapeViewIdentityFn()
}

// reshapeViewIdentityFn is a package var (not a const) so tests inject a DIFFERENT
// identity and prove the view path re-keys on a schema/loader change.
var reshapeViewIdentityFn = func() string {
	sum := sha256.Sum256([]byte(moduleIdentity(specModulePath) + "\x00" + moduleIdentity(sdkModulePath)))
	return hex.EncodeToString(sum[:8])
}

// reshapeViewPath is the content-addressed derived-view directory for a pristine cache
// export: <cachePath>.view.<identity>. The view holds the SAME tree reshaped to the
// current schema; the pristine export is never mutated.
func reshapeViewPath(cachePath string) string {
	return cachePath + ".view." + reshapeViewIdentity()
}

// reshapeViewMarker marks a fully-built derived view. Its presence (after the atomic
// rename of the completed copy) is the completeness proof: a half-built view has no
// marker and is rebuilt, so a crash mid-reshape never publishes a torn view.
const reshapeViewMarker = ".charly-view-ok"

// DeriveRepoView returns a directory holding the repo's project files reshaped to the
// current schema, WITHOUT ever mutating the pristine cache export. The pristine export
// is read-only shared state; the view is materialized once (copy + reshape under a
// per-view flock, published by atomic rename) and reused thereafter. The view path is
// content-addressed (reshapeViewPath) so a schema/loader change re-keys. The migrate
// callback is the registry-coupled command:migrate leg (seams.MigrateCache); passing it
// in keeps this mechanism kind-blind and unit-testable.
func DeriveRepoView(cachePath string, migrate func(path string) error) (string, error) {
	viewPath := reshapeViewPath(cachePath)
	if _, err := os.Stat(filepath.Join(viewPath, reshapeViewMarker)); err == nil {
		return viewPath, nil
	}
	// Re-entry guard (see autoMigratedRepos): admit each view once per process.
	if !markRepoAutoMigrating(viewPath) {
		if _, err := os.Stat(filepath.Join(viewPath, reshapeViewMarker)); err == nil {
			return viewPath, nil
		}
		return cachePath, nil
	}
	release, err := lock.AcquireFileLock(viewPath+".lock", true)
	if err != nil {
		// NEVER fall back to reshaping cachePath in place: the pristine export is
		// read-only shared state, and rewriting it is the defect this function exists to
		// remove. A lock/IO failure is a hard load failure — the pristine tree stays
		// untouched, so a retry derives cleanly.
		return "", fmt.Errorf("deriving reshape view of %s: acquiring view lock: %w", cachePath, err)
	}
	defer func() { _ = release() }()
	// Re-check under the lock: a concurrent first-misser may have built the view.
	if _, err := os.Stat(filepath.Join(viewPath, reshapeViewMarker)); err == nil {
		return viewPath, nil
	}
	tmpPath := viewPath + ".tmp"
	_ = os.RemoveAll(tmpPath)
	if err := copyTree(cachePath, tmpPath); err != nil {
		_ = os.RemoveAll(tmpPath)
		return "", fmt.Errorf("deriving reshape view of %s: %w", cachePath, err)
	}
	if err := migrate(tmpPath); err != nil {
		_ = os.RemoveAll(tmpPath)
		return "", fmt.Errorf("reshaping derived view of %s: %w", cachePath, err)
	}
	if err := os.WriteFile(filepath.Join(tmpPath, reshapeViewMarker), []byte(reshapeViewIdentity()+"\n"), 0o644); err != nil {
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

// EnsureRepoDownloaded downloads the repo if not already cached, then returns a
// CONTENT-ADDRESSED reshaped VIEW of it (DeriveRepoView) — the pristine shared cache is
// NEVER mutated. The view is keyed by the schema+loader identity (reshapeViewPath), so a
// schema or reshape change re-keys; an already-built view is reused.
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
	// Derive a content-addressed reshaped VIEW — never mutate the pristine shared cache.
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
	// coordinate — per-entity-version arbitration (and any diagnostic) happens AFTER fetch in
	// ScanCandyFromLocal, so a re-tag of an unchanged candy no longer reports here.
	//
	// SCOPE IS PER BOX (the version rule: a box is an independent, immutable composition). The
	// same bare ref is composed by MANY boxes across the assembled closure, and two such boxes
	// legitimately pin it at different tags — that is NOT a conflict. So every ref is collected
	// with the SCOPE(S) that named it, and the arbiter (loaderkit.PickCandyVersion) reports only
	// within one scope. `byScope` maps (repo, tag) -> scope label -> set of bare refs, replacing
	// the former single global `(repo, tag) -> refs` map that keyed candidates by bare ref ALONE
	// and merged every independent box into one set (charly#735 §9).
	type repoVer struct{ repo, ver string }
	pairs := make(map[repoVer]map[string]bool)         // (repo, git-tag) -> set of bare refs (the FETCH set)
	referrers := make(map[repoVer]map[string][]string) // (repo, git-tag) -> bare ref -> SCOPE labels that named it
	// Track resolved default branches per repo (to avoid duplicate git queries)
	defaultBranches := make(map[string]string)

	// addRef records that `scope` names `ref`. A ref may be named by MANY scopes (independent
	// boxes legitimately pin the same candy at different tags), so the scope set travels with
	// the (repo, git-tag) fetch coordinate and the post-fetch arbiter reports only within ONE
	// scope. This replaces the former single global `(repo, tag) -> refs` map that keyed
	// candidates by bare ref ALONE and merged every independent box into one set (charly#735 §9).
	addRef := func(ref, scope string) error {
		if !spec.IsRemoteCandyRefString(ref) {
			return nil
		}
		parsed := spec.ParseRemoteRef(ref)
		bareRef := spec.BareCandyRef(ref)
		version := parsed.Version
		if version == "" {
			// No version specified — resolve to the repo's DEFAULT BRANCH, not a tag.
			// A version-less ref (`@github.com/owner/repo`, no `:tag`) means "the current
			// tip of that repo", so it must TRACK the default branch. Resolving to the
			// newest tag silently served a STALE build whenever the repo had newer work on
			// its default branch than on its last tag, and — worse — the tag's provider
			// SHADOWED a project's own explicit pin for the same word (opencharly/charly#715).
			// The default branch is a MUTABLE ref, so it re-resolves per access
			// (refs.IsMutableRef) and a stale export refreshes; a repo with no resolvable
			// default branch errors loudly rather than silently pinning something.
			if branch, ok := defaultBranches[parsed.RepoPath]; ok {
				version = branch
			} else {
				repoURL := refs.RepoGitURL(parsed.RepoPath)
				branch, err := resolveDefaultBranch(repoURL)
				if err != nil {
					return fmt.Errorf("%s: cannot resolve default branch for %s: %w", scope, parsed.RepoPath, err)
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
		if referrers[key] == nil {
			referrers[key] = make(map[string][]string)
		}
		referrers[key][bareRef] = appendUnique(referrers[key][bareRef], scope)
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
	//
	// Each box visits under its FULLY-QUALIFIED name (namespace path, e.g. "cachyos.cachyos") so
	// two same-leaf boxes in different namespaces are distinct scopes. `boxScopeRefs` records, per
	// box scope, that box's OWN authored candy refs, so a layer's require:/candy: (walked below)
	// is attributed to the SCOPE of the box whose composition pulled that layer in — the
	// authoritative unit ("multiple layers INSIDE one box"). Keying by scope (not by leaf box name)
	// keeps a namespaced box's closure attributed to its qualified scope.
	collected := map[*spec.Config]map[string]bool{}
	boxScopeRefs := map[string][]string{} // box scope label -> its authored candy refs
	var collectBox func(c *spec.Config, nsPath, name string) error
	collectBox = func(c *spec.Config, nsPath, name string) error {
		qualified := joinScopeName(nsPath, name)
		scope := "box=" + qualified
		seen := collected[c]
		if seen == nil {
			seen = map[string]bool{}
			collected[c] = seen
		}
		if seen[qualified] {
			return nil
		}
		seen[qualified] = true
		img, ok := c.BoxConfig(name)
		if !ok {
			return nil // external OCI base or unknown name — no candies to collect
		}
		for _, candyRef := range img.Candy {
			if err := addRef(candyRef, scope); err != nil {
				return err
			}
			boxScopeRefs[scope] = append(boxScopeRefs[scope], candyRef)
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
				// Preserve the namespace context: a bare base/builder edge stays inside the
				// visiting namespace (nsPrefix == ""), a qualified one descends from it. The walk
				// re-enters through `tc` (the resolved sub-Config) carrying only the PATH.
				if err := collectBox(tc, joinScopeName(nsPath, nsPrefix(ref)), spec.LeafName(ref)); err != nil {
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
			if err := collectBox(cfg, "", imgName); err != nil {
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
				if err := collectBox(tc, nsPrefix(name), spec.LeafName(name)); err != nil {
					return nil, err
				}
			}
		}
		// kind:local templates compose remote @-ref candies too; they are a property of the
		// namespace that declares them (never a base/builder, so unreachable from the box walk),
		// so attribute them to that namespace's scope label — a distinct composition, not a box.
		for tplName, body := range cfg.Local {
			r, rerr := seams.ResolveLocal(body)
			if rerr != nil || r == nil {
				continue
			}
			for _, candyRef := range r.Candy {
				if err := addRef(candyRef, "kind:local="+tplName); err != nil {
					return nil, err
				}
			}
		}
	}

	// Scan EVERY layer's require:/candy: fields. Each remote ref is attributed to the SCOPE of the
	// box(es) whose candy closure pulled that layer in — resolved by walking each box's own
	// include:/require: graph from its authored candy list. `layers` is the FLAT project candy set
	// (every box's closure merged), so the same layer is ONE entry reached by several boxes; it
	// therefore carries the UNION of those boxes' scope labels. A ref reachable from a box that
	// was never collected (a namespace's unreferenced image, or a namespace scan's own local set)
	// lands in the `unowned` scope, where it stays silent unless another unowned ref disagrees.
	layerScopes := buildLayerScopeIndex(layers, boxScopeRefs)
	for candyName, layer := range layers {
		scopes := layerScopes[candyName]
		if len(scopes) == 0 {
			// Ownership unknown (a local candy no collected box composes — e.g. a namespace-local
			// candy). Its refs form their OWN scope, so they stay SILENT: the version rule scopes
			// conflicts to a box, and an unknown owner has no box to conflict within. This is the
			// deliberate non-conservative direction the ruling requires — cross-composition
			// differences are not a defect.
			scopes = []string{"layer=" + candyName}
		}
		for _, dep := range layer.GetRequire() {
			for _, scope := range scopes {
				if err := addRef(dep.Raw, scope); err != nil {
					return nil, err
				}
			}
		}
		for _, ref := range layer.GetIncludedCandy() {
			for _, scope := range scopes {
				if err := addRef(ref.Raw, scope); err != nil {
					return nil, err
				}
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
		if err := addRef(ref, "deploy=add_candy"); err != nil {
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
			RepoPath:     key.repo,
			Version:      key.ver,
			Refs:         refList,
			RefReferrers: referrers[key],
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

// resolveDefaultBranch resolves a repo's default branch for a VERSION-LESS remote ref.
// Package var (test seam) defaulting to the centralized cached git layer, so the
// versionless-ref resolution is unit-testable offline.
var resolveDefaultBranch = func(repoURL string) (string, error) {
	return gitClient().DefaultBranch(repoURL)
}

// appendUnique appends s to list unless it is already present. The referrer list is a SCOPE
// SET — one entity naming a ref twice (e.g. both require: and candy:) is still one scope.
func appendUnique(list []string, s string) []string {
	for _, existing := range list {
		if existing == s {
			return list
		}
	}
	return append(list, s)
}

// nsPrefix returns the namespace-path portion of a (possibly qualified) ref — everything before
// the final "." — or "" for a bare ref. "a.b.c" -> "a.b"; "fedora" -> "".
func nsPrefix(ref string) string {
	if i := strings.LastIndexByte(ref, '.'); i > 0 {
		return ref[:i]
	}
	return ""
}

// joinScopeName composes a namespace PATH (possibly empty) with a leaf name, so a box visited
// through an import keeps its fully-qualified identity: joinScopeName("cachyos", "cachyos") =
// "cachyos.cachyos", joinScopeName("", "arch") = "arch". Two same-leaf boxes in different
// namespaces thus get DISTINCT scope labels.
func joinScopeName(nsPath, leaf string) string {
	if nsPath == "" || leaf == "" {
		return nsPath + leaf
	}
	return nsPath + "." + leaf
}

// buildLayerScopeIndex resolves, for every candy in the FLAT project candy set, the set of BOX
// scopes whose candy closure reaches it. It walks each collected box's own authored candy refs
// transitively through the layer include:/require: graph — the same closure the build's
// ResolveCandyOrder computes — so a layer pulled in by exactly one box is attributed to that
// box, and a layer pulled in by several carries all of them. This is what makes the authoritative
// unit exact: ">=2 layers INSIDE one box" is a same-scope disagreement, while two independent
// boxes pinning different tags share no scope and stay silent.
func buildLayerScopeIndex(layers map[string]spec.CandyReader, boxScopeRefs map[string][]string) map[string][]string {
	// Reverse index: how many (and which) scopes reach each candy. Bounded by the graph size.
	members := map[string]map[string]bool{} // candy (bare name/ref) -> set of scope labels
	// Iterate scopes in sorted order for determinism; the result is a set, but a stable order
	// keeps a future consumer's output reproducible.
	scopes := make([]string, 0, len(boxScopeRefs))
	for scope := range boxScopeRefs {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	for _, scope := range scopes {
		visited := map[string]bool{}
		var walk func(ref string)
		walk = func(ref string) {
			name := spec.BareCandyRef(ref)
			if name == "" || visited[name] {
				return
			}
			visited[name] = true
			if members[name] == nil {
				members[name] = map[string]bool{}
			}
			members[name][scope] = true
			layer, ok := layers[name]
			if !ok {
				return
			}
			for _, dep := range layer.GetIncludedCandy() {
				walk(dep.Raw)
			}
			for _, dep := range layer.GetRequire() {
				walk(dep.Raw)
			}
		}
		for _, ref := range boxScopeRefs[scope] {
			walk(ref)
		}
	}
	out := make(map[string][]string, len(members))
	for name, set := range members {
		labels := make([]string, 0, len(set))
		for s := range set {
			labels = append(labels, s)
		}
		sort.Strings(labels)
		out[name] = labels
	}
	return out
}
