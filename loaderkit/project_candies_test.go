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
