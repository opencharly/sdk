package loaderkit

// namespace_node_disjoint.go — D-VAL-4 (DESIGN.md §12.4): "Namespaces and top-level node names
// are disjoint."
//
// WHY the loader must enforce it: the reference grammar makes the FIRST segment of a dotted name a
// namespace when one of that name exists (DESIGN.md §11.1 Read, D-REF-1, D-LOAD-6), so a document
// that both imports `tools` and defines a top-level node `tools` gives ONE word two meanings —
// `tools.ripgrep` resolves through the namespace while a bare `tools` names the node. No CUE
// definition can see that collision: `import:` is a list of `alias: ref` pairs, so an alias is a
// VALUE inside an element, never a key the closed `#NodeDoc` could unify against the document's
// own top-level keys. The design states the rule in its §12.4 table with `rules.cue` as the proof
// on the design's examples (design/example/rules.cue, design/negative/namespace-is-node.yaml);
// this file is core's enforcement of the same rule.
//
// WHY the scope is the PROJECT and not one document: the namespace table a reference resolves in
// belongs to the project — the root document, every flat import it pulls in (they append their
// documents to the same spec.LoadedProject, so they share its namespace table and its top-level
// node names), and every discovered manifest (DESIGN.md §11.1 Read names "the project root, its
// `discover:` documents and its `import:` closure"). A per-file check would miss the flat-import
// and discover cases; a namespaced import's own project is a SEPARATE scope and is checked by its
// own walk-project call.

import (
	"fmt"
	"path/filepath"

	"github.com/opencharly/spec/spec"
)

// locatedNode is one top-level node name together with the document that declares it — the
// diagnostic needs both halves, and the rule is judged on the name alone.
type locatedNode struct {
	at   string
	name string
}

// checkNamespaceNodeDisjoint refuses a project whose import namespace is also a top-level node
// name in the same scope. nsByAlias is the walk's own namespace index for this project — the ONE
// place every namespaced import's alias is recorded (walkFile, flat imports included) — so this
// check reads the scope resolution reads instead of re-deriving it.
func checkNamespaceNodeDisjoint(lp *spec.LoadedProject, nsByAlias map[string]*spec.LoadedProject) error {
	if len(nsByAlias) == 0 {
		return nil
	}
	for _, node := range topLevelNodes(lp) {
		if _, ok := nsByAlias[node.name]; ok {
			return fmt.Errorf("%s: import namespace %q is also a top-level node name — a namespace and a top-level node name are disjoint (DESIGN.md §12.4 D-VAL-4); rename one", node.at, node.name)
		}
	}
	return nil
}

// topLevelNodes lists every top-level node name in one project scope, with its declaring document:
// the walk's own documents (the root file and its flat imports, in walk order) first, then every
// discovered manifest.
func topLevelNodes(lp *spec.LoadedProject) []locatedNode {
	var out []locatedNode
	for _, doc := range lp.Docs {
		for _, node := range doc.Project.Nodes {
			out = append(out, locatedNode{at: doc.SrcLabel, name: node.Name})
		}
	}
	for _, dm := range lp.Discovered {
		at := filepath.Join(dm.Dir, dm.Manifest)
		for _, doc := range dm.Docs {
			for _, node := range doc.Nodes {
				out = append(out, locatedNode{at: at, name: node.Name})
			}
		}
	}
	return out
}
