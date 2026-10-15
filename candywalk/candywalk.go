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
// The package carries only the CONTRACT module + the pure kit (github.com/opencharly/spec for
// ParseRemoteRef (F4.3), github.com/opencharly/sdk/kit for RepoLayoutDirs (F4.4)) — never charly,
// never a mechanism kit with registry/host coupling. It returns the raw per-node kind
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

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
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
	return collectEntitiesWithRemote(roots, nil)
}

// CollectEntitiesRemote extends CollectEntities with the candy de-submodule cutover's
// remote-ref-aware discovery (Phase 3): every @github.com/opencharly/... ref found in the walked
// files' require:/candy: lists is resolved through the caller-supplied seam and the fetched
// repo's own candy entities (candy/<name>/charly.yml or the repo-root charly.yml for config
// candies) are unioned into the result, TRANSITIVELY — a fetched repo's own require:/candy:
// lists may surface new refs, which are fetched in turn, until closure (the same fix-point the
// runtime scan materialises). Entity.SourceRoot carries the fetched repo dir for remote entities
// so schema reads resolve against the fetched tree, not the local project root.
func CollectEntitiesRemote(roots []Root, resolve RemoteResolver) ([]Entity, error) {
	return collectEntitiesWithRemote(roots, resolve)
}

// collectEntitiesWithRemote is the shared walker behind CollectEntities (resolve == nil) and
// CollectEntitiesRemote (resolve != nil). When resolve is nil it is exactly the pure local
// FS walk of candy/ + box/. When resolve is set, every @github.com/opencharly/... ref found in
// the walked files (local AND fetched) is resolved and the fetched repo's entities unioned in,
// fix-pointing until no new refs surface.
func collectEntitiesWithRemote(roots []Root, resolve RemoteResolver) ([]Entity, error) {
	var out []Entity
	seenRefs := map[string]bool{} // repoPath:version -> fetched (or attempted)

	// walkOne walks ONE repo root's candy/ + box/ dirs, appends entities, and returns any new
	// @github refs found in its files (only when resolve is active).
	walkRoot := func(r Root) ([]string, error) {
		var newRefs []string
		for _, kind := range kit.RepoLayoutDirs {
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
			if pr.RepoPath == "" {
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

// remoteRefRe matches a remote candy ref in a require:/candy: list — ANY ref form, not CalVer
// alone. The path class stops at the first `:` so the version stays unambiguous even when the ref
// itself contains `/`, `.` and `-` (a branch such as `feat/kubevirt-marketplace-family-2` is
// indistinguishable from the path by character class alone — which is exactly why the former
// `:v[0-9]+\.[0-9]+\.[0-9]+` anchor was used, and exactly why branches and SHAs were dropped).
// The ref class stops at the delimiters that end a YAML scalar, so a quoted (`'…'`) or
// flow-sequence (`[…, …]`) entry yields the bare token.
//
// `<` and `>` are excluded from BOTH classes because they appear in no repository name and no
// version: they exist only in the notation the docs use to DESCRIBE a ref
// (`@github.com/opencharly/<kind>-<name>:<tag>`), and prose about refs is not a ref. Without the
// exclusion that token parses as a repo path plus a version, reaches the resolver, and is
// `git ls-remote`d as a URL — measured: `charly marketplace generate|drift` died in `collect entities`
// on exactly such a line, taken from a marketplace CHANGELOG. Excluding them keeps every real form,
// including the branch and SHA cases the anchor above was dropped for.
//
// The VERSION class additionally requires two characters, and that is not cosmetic: excluding `<>`
// alone TRUNCATES a placeholder rather than rejecting it. In
// `@github.com/opencharly/layer-omarchy-base/candy/omarchy-base:v<version>` — the same notation with
// the placeholder in the version instead of the repo name — the repo path is real, so the match stops
// just before `<` and survives with a one-character version `v`, which then reaches the resolver as a
// ref that cannot exist (measured: `could not resolve ref "v"`). Every real version is at least two
// characters — a CalVer tag, a branch, a SHA — so requiring two rejects the stub without narrowing
// any legitimate form. See opencharly/sdk#408.
var remoteRefRe = regexp.MustCompile(`@github\.com/opencharly/[^\s'"\],}#:<>` + "`" + `]+:[^\s'"\],}#:<>` + "`" + `]{2,}`)

// acceptableVersion reports whether a candidate's VERSION is one of the closed set of REAL ref
// forms. The scanner (remoteRefRe) FINDS; this DECIDES. That split is the point: a character class
// can only guess, while an allowlist decides. opencharly/sdk#405 excluded two characters, #408/#409
// imposed a length, and each was derived from one observed prose instance — `vTAG` is four
// characters, so no length constant can ever reach it, which is the proof that the constant was
// never the defect.
//
// A version that CLAIMS the tag namespace (`v` followed by a digit) must be tag-shaped, and that one
// rule kills the whole `v2026.xxx.yyyy` family: it claims `v<digit>` and is not CalVer, so it is
// prose. Anything else is accepted only as a branch or a commit — which keeps opencharly/sdk#322
// fixed, because a branch or a SHA is exactly what a fail-closed pattern used to drop.
func acceptableVersion(v string) bool {
	if len(v) > 1 && v[0] == 'v' && v[1] >= '0' && v[1] <= '9' {
		if calVerRe.MatchString(v) {
			return true
		}
		// A dot means the token is CLAIMING the CalVer/semver form, and failing that claim makes it
		// prose (`v2026.xxx.yyyy`). Without a dot it is a branch that merely happens to start with
		// `v<digit>` — `v2-feature` — and rejecting that is the opencharly/sdk#322 defect, where a live
		// branch was dropped with no error. Both are pinned in the fixture; neither is asserted in
		// prose only, which is what the review round taught.
		return !strings.Contains(v, ".") && branchRe.MatchString(v)
	}
	// The same rule one `v` to the left: a token SHAPED like CalVer (a four-digit year, then a dot)
	// must BE CalVer. The `v`-less placeholder `2026.xxx.yyyy` is the same prose in the same notation
	// minus its `v`, and the branch arm below would otherwise wave it through.
	if plainCalShapeRe.MatchString(v) {
		return plainCalRe.MatchString(v)
	}
	// A version whose components are all one or two characters AND whose FIRST component is letters is
	// placeholder-shaped, not a ref: `vx.y.z` is the residual filed as opencharly/sdk#412. The
	// letter-first requirement is the whole of this rule's precision, and it came from a review round:
	// without it the pattern also swallowed ordinary version-shaped tokens — `1.2.3`, `a.b.c` — which
	// are real and must resolve. Digits where the placeholder has a letter stub is the difference. Note what this is NOT — a context rule.
	// "Inside a code span means prose" was the obvious fix and it is wrong: this project's own fixture
	// requires a backticked REAL ref to be collected clean, so rejecting spans would drop live refs and
	// re-open opencharly/sdk#322. The shape is what tells them apart: a real version's components are
	// digits (a tag) or words (a branch), never single letters.
	if stubVersionRe.MatchString(v) {
		return false
	}
	return branchRe.MatchString(v) || shaRe.MatchString(v)
}

var (
	// CalVer, the Go-module CalVer and semver are one shape with a differing segment count.
	calVerRe = regexp.MustCompile(`^v\d+(\.\d+){1,2}(-[A-Za-z0-9.]+)?$`)
	// A branch is lowercase by convention; `feat/x`, `main`, `latest`, and a plain `2026.108.56`
	// tag all pass, while `vTAG` and a lone `v` do not.
	branchRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]+$`)
	shaRe    = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	// A plain CalVer tag, and the SHAPE that claims it (four digits then a dot) — the `v`-less
	// placeholder `2026.xxx.yyyy` matches the shape and must therefore be rejected.
	plainCalRe      = regexp.MustCompile(`^\d{4}\.\d+\.\d+$`)
	plainCalShapeRe = regexp.MustCompile(`^\d{4}\.`)
	// `vx.y.z` — two or more dot-separated components of one or two characters each (opencharly/sdk#412).
	stubVersionRe = regexp.MustCompile(`^[A-Za-z]{1,2}(\.[A-Za-z0-9]{1,2}){2,}$`)
)

// collectRefs returns every remote candy ref found in one file (the @-prefixed require/candy
// refs only — package names and bare candy names are never remote refs). Refs that point INTO
// the charly repo itself (@github.com/opencharly/charly/candy/<name> — the pre-cutover in-repo
// form, still carried by box submodule repos until their own cutover) are NOT standalone candy
// repos and are skipped: they resolve to the LOCAL charly checkout's candy dir, which the local
// FS walk already surfaces (or, after Phase 4, are stale refs swept by the box repos' own
// cutover).
//
// WHY THE PATTERN ACCEPTS EVERY REF FORM AND NOTHING FAILS CLOSED. The former CalVer-only pattern
// dropped a branch (`:feat/…`) or a commit SHA with no error and no warning, so a fail-closed
// generator (`charly marketplace generate`) validated a STALE entity and reported a failure the
// author had no way to fix — the ref that would have corrected it was never collected
// (opencharly/sdk#322). Collecting every form is the fix; the loader already re-resolves a MUTABLE
// ref on each access, so handing it over is what makes the generator see the branch under test.
//
// The issue's second, "minimum" arm — hard-error on a declared ref the collector cannot name — was
// MEASURED and REJECTED as a text-level check: this function scans RAW file bytes, comments
// included, and a sweep of the org's tracked charly.yml files finds 42 distinct VERSIONLESS
// `@github.com/opencharly/…` tokens living in PROSE (a `layer-nodejs` mention in a comment, a
// `distro-*` glob, a trailing-backtick example). Failing closed on those would break every tree
// that documents a repo path. A versionless token is therefore not evidence of a declared ref;
// only a token carrying a `:version` is, and every such form is collected — provided its version is
// one of the real shapes, which acceptableVersion decides (opencharly/sdk#410).
func collectRefs(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, ref := range remoteRefRe.FindAllString(string(raw), -1) {
		pr := parseRemoteRef(ref)
		if pr.RepoPath == "" || pr.RepoPath == "github.com/opencharly/charly" {
			continue
		}
		// Prose notation is dropped here, at the scan, rather than left for the resolver to choke on
		// (`could not resolve ref "v2026.xxx.yyyy"`). See acceptableVersion.
		if !acceptableVersion(pr.Version) {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// parseRemoteRef is the canonical spec.ParseRemoteRef (F4.3 parser consolidation: the
// pre-cutover in-repo form (@github.com/opencharly/charly/candy/<name>) is excluded by the
// caller — collectRefs skips RepoPath == github.com/opencharly/charly).
func parseRemoteRef(ref string) *spec.ParsedRef {
	return spec.ParseRemoteRef(ref)
}

// ReadEntityFile decodes one charly.yml into its top-level nodes. The file is a map of entity
// NAME to a single kind key (name-first shape); each (name, kind, value) triple is one Entity.
// A charly.yml that does not fit the name-first shape (a multi-doc bed, a template) is skipped
// rather than failing the whole discovery. Scalar top-level keys (the standalone repos' prepended
// `version: <calver>` schema header) are tolerated and ignored — they are not entities.
func ReadEntityFile(path string, r Root, relDir string) ([]Entity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		// Not catalog content — skip it rather than failing the whole walk.
		return nil, nil //nolint:nilerr // intentionally tolerant: non-catalog shapes are skipped
	}
	var out []Entity
	for name, node := range doc {
		// The node's value is a mapping of KIND discriminators (`candy:`, `skill:`, …) to their
		// value bodies. A SCALAR top-level key (the prepended `version: <calver>` header, or a
		// directive-style key) is not an entity — skip it. This is the shape the pre-cutover
		// in-repo manifests (name-first with no header) and the standalone repos (header +
		// name-first) both share, so the same reader walks both.
		if node.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			kind := node.Content[i].Value
			out = append(out, Entity{Name: name, Namespace: r.Namespace, Dir: relDir, Kind: kind, Value: *node.Content[i+1]})
		}
	}
	return out, nil
}
