// Package candywalk is the SHARED entity-discovery walk over a charly repo: the superproject
// plus every box/<distro> submodule, reading every candy/ + box/ charly.yml and returning one
// Entity per top-level node. It is the ONE discovery abstraction (R3) both out-of-process
// generator plugins use — candy/plugin-docs (the docs site generator) and candy/plugin-marketplace
// (the harness/marketplace generator) — so the two never fork their own directory conventions.
//
// Discovery is PURE FILESYSTEM walking + os.ReadFile — it never shells out to git, never runs
// charly commands. "Defined, not default-active": it returns every DEFINED entity (what an author
// wrote down), never the resolved/enabled set (the resolver's surfaces are wrong for a catalog —
// `charly box list boxes` resolves through main's import: closure and omits every debian.* and
// ubuntu.* box). Each repo (the superproject + each checked-out box/<distro> submodule) is walked
// as its own root and the results unioned; an uninitialised submodule (no charly.yml) is skipped.
//
// The candy de-submodule cutover (Phases 1-2) moved every candy to a standalone kind-prefixed
// repo (layer-*, pod-*, vm-*, plugin-*). Phase 4 deletes the in-repo candy/ dirs, after which the
// local FS walk alone finds only the residual fixtures. CollectEntitiesRemote extends discovery
// with a REMOTE-REF-AWARE mode: it also collects every @github.com/opencharly/... ref from the
// walked files' require:/candy: lists, resolves each repo+tag via a CALLER-SUPPLIED resolver seam
// (out-of-process generators use spec/refs.DownloadRepo; an in-proc caller could supply the
// loader's EnsureRepoDownloaded), and walks each fetched repo's candy dir (or repo-root
// charly.yml for config candies) — fix-pointing until no new refs surface, the same transitive
// closure the runtime scan materialises. The seam keeps this kit import-free: candywalk itself
// still carries NO charly or sdk import — only the stdlib + yaml.v3.
//
// The kit carries NO charly import — only the stdlib + yaml.v3. It returns the raw per-node kind
// discriminator + value node; the caller owns the projection into its own types (the docs plugin
// decodes `candy:` into its candyView/boxView; the marketplace plugin decodes `skill:`/`hook:`/
// `marketplace:` into the generated spec types).
package candywalk

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnifiedFileName is the one entity filename (the project rulebook's "one filename charly.yml").
const UnifiedFileName = "charly.yml"

// Root is one project tree to walk: the superproject, or a box/<distro> submodule.
type Root struct {
	Namespace string // "" for the superproject, else the box/<distro> submodule name
	Dir       string // absolute path
}

// Entity is one top-level node of a discovered charly.yml, regardless of kind. The value node is
// the node's kind value (the body under `<name>: {<kind>: …}`).
//
// Value is a VALUE yaml.Node, not a pointer: yaml.v3's decode into
// `map[string]map[string]*yaml.Node` silently drops nested structs when the caller then Decodes
// the leaf (a plugin-fleet candy's `plugin:` block came back nil), while the value-node decode
// round-trips faithfully. The value-node form is what the original plugin-docs walk used.
//
// SourceRoot is the ABSOLUTE path of the repo root this entity was read from: "" for a local
// superproject/box root (the caller knows the local root it passed in), or the fetched repo cache
// dir for a REMOTE entity from CollectEntitiesRemote. Schema reads and other per-entity file
// lookups must resolve relative to SourceRoot (when non-empty) rather than the local project root.
type Entity struct {
	Name       string    // the top-level node name (as authored)
	Namespace  string    // "" for the superproject, else the box/<distro> submodule name
	Dir        string    // directory holding this charly.yml, relative to the repo root
	Kind       string    // the node's kind discriminator ("candy", "skill", "hook", "marketplace", …)
	Value      yaml.Node // the kind value body
	SourceRoot string    // absolute repo root ("" = local root; else the fetched remote cache dir)
}

// RepoRoots enumerates the superproject plus every box/<distro> submodule that is actually
// checked out. A submodule with no charly.yml is an uninitialised gitlink — skipped rather than
// failed, so discovery still runs in a partially-initialised clone.
func RepoRoots(root string) ([]Root, error) {
	roots := []Root{{Namespace: "", Dir: root}}
	boxDir := filepath.Join(root, "box")
	ents, err := os.ReadDir(boxDir)
	if err != nil {
		if os.IsNotExist(err) {
			return roots, nil
		}
		return nil, fmt.Errorf("read box/: %w", err)
	}
	for _, de := range ents {
		if !de.IsDir() {
			continue
		}
		sub := filepath.Join(boxDir, de.Name())
		if _, err := os.Stat(filepath.Join(sub, UnifiedFileName)); err != nil {
			continue // uninitialised submodule
		}
		roots = append(roots, Root{Namespace: de.Name(), Dir: sub})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Namespace < roots[j].Namespace })
	return roots, nil
}

// RemoteResolver resolves one remote candy ref to a LOCALLY FETCHED repo directory. It is the
// caller-supplied seam that keeps this kit import-free: an out-of-process generator supplies
// spec/refs.DownloadRepo (or a wrapper), an in-proc caller supplies the loader's
// EnsureRepoDownloaded. The returned dir must hold the fetched repo export (the same shape the
// runtime's repo cache produces) so the walker can read its candy/ + root charly.yml.
type RemoteResolver func(repoPath, version string) (fetchedDir string, err error)

// CollectEntities reads every entity node across every repo root, for the two discovery
// directories the loader uses (candy/ and box/). Discovery is by directory convention
// (candy/<name>/charly.yml, box/<name>/charly.yml) — the same two the loader uses — so the
// result is exactly what an author has written down.
func CollectEntities(roots []Root) ([]Entity, error) {
	return collectEntitiesWithRemote(roots, nil, "")
}

// CollectEntitiesRemote extends CollectEntities with the candy de-submodule cutover's
// remote-ref-aware discovery (Phase 3): every @github.com/opencharly/... ref found in the walked
// files' require:/candy: lists is resolved through the caller-supplied seam and the fetched
// repo's own candy entities (candy/<name>/charly.yml or the repo-root charly.yml for config
// candies) are unioned into the result, TRANSITIVELY — a fetched repo's own require:/candy:
// lists may surface new refs, which are fetched in turn, until closure (the same fix-point the
// runtime scan materialises). Entity.SourceRoot carries the fetched repo dir for remote entities
// so schema reads resolve against the fetched tree, not the local project root.
//
// rootRefs is the seed: refs found in the LOCAL files' require:/candy: lists before any fetch.
func CollectEntitiesRemote(roots []Root, resolve RemoteResolver) ([]Entity, error) {
	return collectEntitiesWithRemote(roots, resolve, "")
}

// collectEntitiesWithRemote is the shared walker behind CollectEntities (resolve == nil) and
// CollectEntitiesRemote (resolve != nil). When resolve is nil it is exactly the pure local
// FS walk of candy/ + box/. When resolve is set, every @github.com/opencharly/... ref found in
// the walked files (local AND fetched) is resolved and the fetched repo's entities unioned in,
// fix-pointing until no new refs surface.
func collectEntitiesWithRemote(roots []Root, resolve RemoteResolver, _ string) ([]Entity, error) {
	var out []Entity
	seenRefs := map[string]bool{} // repoPath:version -> fetched (or attempted)

	// walkOne walks ONE repo root's candy/ + box/ dirs, appends entities, and returns any new
	// @github refs found in its files (only when resolve is active).
	walkRoot := func(r Root) ([]string, error) {
		var newRefs []string
		for _, kind := range []string{"candy", "box"} {
			dir := filepath.Join(r.Dir, kind)
			ents, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("read %s: %w", dir, err)
			}
			for _, de := range ents {
				if !de.IsDir() {
					continue
				}
				path := filepath.Join(dir, de.Name(), UnifiedFileName)
				found, err := ReadEntityFile(path, r, filepath.Join(kind, de.Name()))
				if err != nil {
					return nil, err
				}
				for i := range found {
					found[i].SourceRoot = r.Dir // local root: the repo root that holds the file
				}
				out = append(out, found...)
				if resolve != nil {
					newRefs = append(newRefs, collectRefs(path)...)
				}
			}
		}
		return newRefs, nil
	}

	// Pass 1: walk every local root (the pre-Phase-4 in-repo state is the default; the remote
	// mode STARTS here too, collecting the local refs as the seed).
	var queue []string
	for _, r := range roots {
		refs, err := walkRoot(r)
		if err != nil {
			return nil, err
		}
		queue = append(queue, refs...)
	}

	// Pass 2: remote fix-point. Resolve each new ref, walk the fetched repo's candy/ + root
	// manifest, union the entities, and queue any refs the fetched files surface.
	if resolve != nil {
		for len(queue) > 0 {
			ref := queue[0]
			queue = queue[1:]
			pr := parseRemoteRef(ref)
			if pr == nil {
				continue
			}
			key := pr.RepoPath + ":" + pr.Version
			if seenRefs[key] {
				continue
			}
			seenRefs[key] = true

			fetched, err := resolve(pr.RepoPath, pr.Version)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", ref, err)
			}
			// A fetched standalone repo holds its candy under candy/<name>/ (plugin repos) OR the
			// manifest at the repo root (config candies). Walk both; the root manifest is read as
			// its own root with Namespace carrying the fetched dir so SourceRoot is unambiguous.
			fetchedRoot := Root{Namespace: key, Dir: fetched}
			refs, err := walkRoot(fetchedRoot)
			if err != nil {
				return nil, fmt.Errorf("walk fetched %s: %w", key, err)
			}
			queue = append(queue, refs...)
			if _, err := os.Stat(filepath.Join(fetched, UnifiedFileName)); err == nil {
				found, rerr := ReadEntityFile(filepath.Join(fetched, UnifiedFileName), Root{Namespace: key, Dir: fetched}, ".")
				if rerr != nil {
					return nil, fmt.Errorf("read fetched root manifest %s: %w", key, rerr)
				}
				for i := range found {
					found[i].SourceRoot = fetched
				}
				out = append(out, found...)
				if resolve != nil {
					queue = append(queue, collectRefs(filepath.Join(fetched, UnifiedFileName))...)
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].SourceRoot != out[j].SourceRoot {
			return out[i].SourceRoot < out[j].SourceRoot
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// remoteRefRe matches the @github.com/opencharly/... remote candy refs in require:/candy: lists.
var remoteRefRe = regexp.MustCompile(`@github\.com/opencharly/[a-zA-Z0-9._/-]+:v[0-9]+\.[0-9]+\.[0-9]+`)

// collectRefs returns every remote candy ref found in one file (the @-prefixed require/candy
// refs only — package names and bare candy names are never remote refs).
func collectRefs(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return remoteRefRe.FindAllString(string(raw), -1)
}

// parseRemoteRef decomposes a @github.com/opencharly/<repo>:v<calver> ref into its repo path and
// version, or nil for anything else.
func parseRemoteRef(ref string) *parsedRemoteRef {
	rest := strings.TrimPrefix(ref, "@")
	idx := strings.LastIndex(rest, ":")
	if idx == -1 {
		return nil
	}
	repo, ver := rest[:idx], rest[idx+1:]
	if repo == "" || ver == "" {
		return nil
	}
	return &parsedRemoteRef{RepoPath: repo, Version: ver}
}

type parsedRemoteRef struct {
	RepoPath string
	Version  string
}

// ReadEntityFile decodes one charly.yml into its top-level nodes. The file is a map of entity
// NAME to a single kind key (name-first shape); each (name, kind, value) triple is one Entity.
// A charly.yml that does not fit the name-first shape (a multi-doc bed, a template) is skipped
// rather than failing the whole discovery.
func ReadEntityFile(path string, r Root, relDir string) ([]Entity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc map[string]map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		// Not catalog content — skip it rather than failing the whole walk.
		return nil, nil //nolint:nilerr // intentionally tolerant: non-catalog shapes are skipped
	}
	var out []Entity
	for name, kinds := range doc {
		// Each key in the node's value map is a kind discriminator (`candy:`, `skill:`, …).
		// A directive-style key (`discover:` …) has a non-map value that yaml.Unmarshal into
		// map[string]yaml.Node already rejected at the file level, so every (kind, value) pair
		// here is an entity node; the loader gates a node with no/several discriminators.
		for kind, value := range kinds {
			out = append(out, Entity{Name: name, Namespace: r.Namespace, Dir: relDir, Kind: kind, Value: value})
		}
	}
	return out, nil
}
