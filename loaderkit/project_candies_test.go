package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestProjectCandiesScanned_FoldsNamespaceCandies is the regression guard for the
// namespace-candy fold: a namespace image (pulled via resolveNamespacedBases) composes its
// candies by BARE name, and those candies live in the namespace's own UnifiedFile. Without
// folding them into the scan, the global candy order fails with "unknown candy" for every
// namespace image that composes a vendored candy (e.g. distro-cachyos's cachyos base
// composing cachyos-base-check from its candy/ discover).
func TestProjectCandiesScanned_FoldsNamespaceCandies(t *testing.T) {
	ns := &spec.UnifiedFile{
		RootDir: "/ns",
		Candy: map[string]json.RawMessage{
			"ns-candy": spec.EncodeInlineCandy(&spec.InlineCandy{
				CandyYAML: spec.CandyYAML{Description: "namespace candy"},
			}),
		},
	}
	root := &spec.UnifiedFile{
		RootDir: "/root",
		Candy: map[string]json.RawMessage{
			"root-candy": spec.EncodeInlineCandy(&spec.InlineCandy{
				CandyYAML: spec.CandyYAML{Description: "root candy"},
			}),
		},
		Namespaces: map[string]*spec.UnifiedFile{"sub": ns},
	}
	got, err := ProjectCandiesScanned(root, "/root", nil)
	if err != nil {
		t.Fatalf("ProjectCandiesScanned: %v", err)
	}
	if _, ok := got["root-candy"]; !ok {
		t.Error("root candy missing from scan")
	}
	if _, ok := got["ns-candy"]; !ok {
		t.Error("namespace candy missing from scan — the namespace fold did not run")
	}
}

// TestProjectCandiesScanned_RootWinsNamespaceCollision pins the root-wins semantics: when the
// root project and a namespace both declare the same bare candy name, the root's candy wins
// (mirroring the materialize root-wins merge).
func TestProjectCandiesScanned_RootWinsNamespaceCollision(t *testing.T) {
	ns := &spec.UnifiedFile{
		RootDir: "/ns",
		Candy: map[string]json.RawMessage{
			"shared": spec.EncodeInlineCandy(&spec.InlineCandy{
				CandyYAML: spec.CandyYAML{Description: "namespace shared"},
			}),
		},
	}
	root := &spec.UnifiedFile{
		RootDir: "/root",
		Candy: map[string]json.RawMessage{
			"shared": spec.EncodeInlineCandy(&spec.InlineCandy{
				CandyYAML: spec.CandyYAML{Description: "root shared"},
			}),
		},
		Namespaces: map[string]*spec.UnifiedFile{"sub": ns},
	}
	got, err := ProjectCandiesScanned(root, "/root", nil)
	if err != nil {
		t.Fatalf("ProjectCandiesScanned: %v", err)
	}
	c, ok := got["shared"]
	if !ok {
		t.Fatal("shared candy missing from scan")
	}
	if c.View.Description != "root shared" {
		t.Errorf("root-wins violated: got %q, want %q", c.View.Description, "root shared")
	}
}

// TestNamespaceAncestors_IsPathScopedNotGlobal pins the ONE guard's semantics (namespace_walk.go):
// a node already on the CURRENT path is refused, but the SAME node reached again on a LATER path —
// a diamond import, or one repo mounted at `arch` + `cachyos.arch` — is admitted once leave() has
// popped it. A global pointer set refuses that second path and silently drops its PluginKinds /
// box views / candy folds (the measured "not defined" failure this rule exists for).
func TestNamespaceAncestors_IsPathScopedNotGlobal(t *testing.T) {
	a := NamespaceAncestors{}
	node := &spec.UnifiedFile{}
	leave, ok := a.Enter(node)
	if !ok {
		t.Fatal("Enter refused a fresh node")
	}
	if _, again := a.Enter(node); again {
		t.Error("Enter admitted a node already on the CURRENT path — a back-edge would recurse forever")
	}
	if _, nilOK := a.Enter(nil); nilOK {
		t.Error("Enter admitted nil")
	}
	leave()
	if _, later := a.Enter(node); !later {
		t.Error("Enter refused the node on a LATER path — the guard is global, so a diamond / multi-alias mount loses every alias after the first")
	}
}

// TestProjectCandiesScanned_TerminatesOnACyclicNamespaceGraph is the opencharly/sdk#352 regression
// guard: uf.Namespaces is cyclic BY DESIGN — the loader mounts a back-imported ancestor as the SAME
// in-progress node (the `main<->sub` mutual import; before opencharly/spec#202 also the self-mount of
// a same-repo subdirectory import). The namespace fold MUST terminate on that back-edge. Before the
// fix this call recursed until `fatal error: stack overflow` (measured live on a project carrying a
// resolved pod target, whose leg runs this scan).
func TestProjectCandiesScanned_TerminatesOnACyclicNamespaceGraph(t *testing.T) {
	sub := &spec.UnifiedFile{
		RootDir: "/sub",
		Candy: map[string]json.RawMessage{
			"sub-candy": spec.EncodeInlineCandy(&spec.InlineCandy{
				CandyYAML: spec.CandyYAML{Description: "sub candy"},
			}),
		},
	}
	root := &spec.UnifiedFile{
		RootDir: "/root",
		Candy: map[string]json.RawMessage{
			"root-candy": spec.EncodeInlineCandy(&spec.InlineCandy{
				CandyYAML: spec.CandyYAML{Description: "root candy"},
			}),
		},
		Namespaces: map[string]*spec.UnifiedFile{"sub": sub},
	}
	// The back-edge: `sub` re-mounts its own importer, as the SAME pointer.
	sub.Namespaces = map[string]*spec.UnifiedFile{"up": root}

	got, err := ProjectCandiesScanned(root, "/root", nil)
	if err != nil {
		t.Fatalf("ProjectCandiesScanned: %v", err)
	}
	if _, ok := got["sub-candy"]; !ok {
		t.Error("namespace candy missing from scan — the fold did not reach the namespace")
	}
}
