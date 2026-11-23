package candywalk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectEntitiesRemote_HonoursSubpath is the R10 gate for opencharly/plugin-marketplace#64:
// a ref that names ONE manifest inside a repo reads THAT manifest, not the whole fetched tree.
//
// FAILS WITHOUT THE CHANGE: the walker walked every fetched tree in full, so the fixture's
// other candies, and the tree's root manifest, were unioned in — which is how a stale box-file
// import at an old tag dragged a whole old distro tree into the corpus.
func TestCollectEntitiesRemote_HonoursSubpath(t *testing.T) {
	root := t.TempDir()
	fetched := t.TempDir()
	write := func(dir, rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The project: one local candy declaring the three refs under test.
	write(root, "candy/keeper/charly.yml", `keeper:
  candy:
    require:
      - '@github.com/opencharly/thing/box/x/charly.yml:v2026.100.0000'
      - '@github.com/opencharly/thing/candy/a:v2026.100.0000'
      - '@github.com/opencharly/thing/nowhere:v2026.100.0000'
`)
	// The fetched tree: a root manifest, two candies, a box file — only the NAMED ones count.
	write(fetched, "charly.yml", "root-thing:\n  candy:\n    description: root\n")
	write(fetched, "candy/a/charly.yml", "candy-a:\n  candy:\n    description: a\n")
	write(fetched, "candy/b/charly.yml", "candy-b:\n  candy:\n    description: b\n")
	write(fetched, "box/x/charly.yml", "box-x:\n  candy:\n    description: x\n")

	resolve := func(repoPath, version string) (string, error) { return fetched, nil }
	ents, err := CollectEntitiesRemote([]Root{{Dir: root}}, resolve)
	if err != nil {
		t.Fatalf("CollectEntitiesRemote: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range ents {
		seen[e.Name] = true
	}
	for _, want := range []string{"candy-a", "box-x", "keeper"} {
		if !seen[want] {
			t.Fatalf("entity %q missing — the named subpath manifests and the local root must be read: %v", want, seen)
		}
	}
	for _, unwanted := range []string{"root-thing", "candy-b"} {
		if seen[unwanted] {
			t.Fatalf("entity %q was read from a manifest the ref did not name — the whole tree must not be walked for a subpath ref: %v", unwanted, seen)
		}
	}
}
