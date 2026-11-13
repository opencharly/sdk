package loaderkit

// namespace_node_disjoint_test.go — D-VAL-4 (DESIGN.md §12.4): "Namespaces and top-level node
// names are disjoint." The design proves the rule on its own examples (design/example/rules.cue,
// design/negative/namespace-is-node.yaml); this file proves the LOADER enforces it, in the scope
// reference resolution actually uses — the project (the root document plus every flat import it
// pulls in) and everything discovered inside it.
//
// The premise the first three tests measure: a document that imports `tools` as a namespace AND
// defines a top-level node `tools` loads CLEAN today, so `tools.ripgrep` resolves through the
// namespace while a bare `tools` names the node — the ambiguity D-VAL-4 forbids. The fourth test
// is the negative control: disjoint names must keep loading.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// disjointSeams is a local-path-only walk harness: ResolveRef resolves a ref against the importing
// file's directory, and the boundary/gate/identity seams are inert (this file judges the
// namespace-scope rule, not CUE validation or the host's side effects).
func disjointSeams() spec.WalkSeams {
	return spec.WalkSeams{
		Parser:   DocParser{},
		Threaded: func() spec.Threaded { return candyThreaded },
		Boundary: func(string, []byte) error { return nil },
		GateDoc:  func(string, []byte) error { return nil },
		ResolveRef: func(ref, baseDir string) (string, string, error) {
			return ref, filepath.Join(baseDir, ref), nil
		},
		RepoIdentity: func(string, string) string { return "" },
	}
}

// writeTree writes the rel-path → body map under a fresh temp root and returns that root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

// wantDisjointError asserts the walk refused the tree for exactly this reason.
func wantDisjointError(t *testing.T, err error, namespace string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Walk accepted a document whose import namespace %q is also a top-level node name (D-VAL-4)", namespace)
	}
	want := `import namespace "` + namespace + `" is also a top-level node name`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Walk error = %q, want it to contain %q", err, want)
	}
}

// TestWalk_RejectsNamespaceThatIsAlsoATopLevelNode: the import and the node live in the same
// document — the shape design/negative/namespace-is-node.yaml records.
func TestWalk_RejectsNamespaceThatIsAlsoATopLevelNode(t *testing.T) {
	root := writeTree(t, map[string]string{
		"charly.yml":       "import:\n  - tools: ./tools\ntools:\n  candy:\n    status: working\n",
		"tools/charly.yml": "ripgrep:\n  candy:\n    status: working\n",
	})
	_, err := Walk(root, nil, "", disjointSeams())
	wantDisjointError(t, err, "tools")
}

// TestWalk_RejectsNamespaceThatIsAlsoAFlatImportedNode: the namespace is imported by the root and
// the colliding node arrives through a FLAT import — one project scope, two files, so a per-file
// check would miss it.
func TestWalk_RejectsNamespaceThatIsAlsoAFlatImportedNode(t *testing.T) {
	root := writeTree(t, map[string]string{
		"charly.yml":       "import:\n  - tools: ./tools\n  - base.yml\n",
		"base.yml":         "tools:\n  candy:\n    status: working\n",
		"tools/charly.yml": "ripgrep:\n  candy:\n    status: working\n",
	})
	_, err := Walk(root, nil, "", disjointSeams())
	wantDisjointError(t, err, "tools")
}

// TestWalk_RejectsNamespaceThatIsAlsoADiscoveredNode: the colliding node is DISCOVERED rather than
// authored in a document the root imports — it still joins the same top-level namespace.
func TestWalk_RejectsNamespaceThatIsAlsoADiscoveredNode(t *testing.T) {
	root := writeTree(t, map[string]string{
		"charly.yml":             "import:\n  - tools: ./tools\ndiscover:\n  - path: candy\n    recursive: true\n",
		"candy/tools/charly.yml": "tools:\n  candy:\n    status: working\n",
		"tools/charly.yml":       "ripgrep:\n  candy:\n    status: working\n",
	})
	_, err := Walk(root, nil, "", disjointSeams())
	wantDisjointError(t, err, "tools")
}

// TestWalk_AcceptsDisjointNamespaceAndNodeNames: the negative control — the rule rejects a
// COLLISION, never a project that merely imports a namespace.
func TestWalk_AcceptsDisjointNamespaceAndNodeNames(t *testing.T) {
	root := writeTree(t, map[string]string{
		"charly.yml":       "import:\n  - tools: ./tools\ntoolbox:\n  candy:\n    status: working\n",
		"tools/charly.yml": "ripgrep:\n  candy:\n    status: working\n",
	})
	lp, err := Walk(root, nil, "", disjointSeams())
	if err != nil {
		t.Fatalf("Walk rejected a project whose namespace and node names are disjoint: %v", err)
	}
	if len(lp.Namespaces) != 1 || lp.Namespaces[0].Alias != "tools" {
		t.Fatalf("namespaces = %+v, want the single mount `tools`", lp.Namespaces)
	}
}
