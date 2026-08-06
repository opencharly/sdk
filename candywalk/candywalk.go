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
// The kit carries NO charly import — only the stdlib + yaml.v3. It returns the raw per-node kind
// discriminator + value node; the caller owns the projection into its own types (the docs plugin
// decodes `candy:` into its candyView/boxView; the marketplace plugin decodes `skill:`/`hook:`/
// `marketplace:` into the generated spec types).
package candywalk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
// the leaf (a plugin-bundle candy's `plugin:` block came back nil), while the value-node decode
// round-trips faithfully. The value-node form is what the original plugin-docs walk used.
type Entity struct {
	Name      string     // the top-level node name (as authored)
	Namespace string     // "" for the superproject, else the box/<distro> submodule name
	Dir       string     // directory holding this charly.yml, relative to the repo root
	Kind      string     // the node's kind discriminator ("candy", "skill", "hook", "marketplace", …)
	Value     yaml.Node  // the kind value body
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

// CollectEntities reads every entity node across every repo root, for the two discovery
// directories the loader uses (candy/ and box/). Discovery is by directory convention
// (candy/<name>/charly.yml, box/<name>/charly.yml) — the same two the loader uses — so the
// result is exactly what an author has written down.
func CollectEntities(roots []Root) ([]Entity, error) {
	var out []Entity
	for _, r := range roots {
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
				out = append(out, found...)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
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
