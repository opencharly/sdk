package candywalk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectEntities_NestedDecodeRoundTrip pins the yaml.v3 VALUE-node trap this kit exists to
// avoid: decoding the discovered Value into a struct with a NESTED sub-mapping (a plugin candy's
// `plugin:` block) must round-trip faithfully. The `*yaml.Node` form of the map decode silently
// drops the nested struct (a plugin-fleet candy's plugin block came back nil); the value-node
// form this kit uses does not.
func TestCollectEntities_NestedDecodeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "candy", "plugin-demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `plugin-demo:
    candy:
        version: 2026.218.1200
        description: A demo plugin candy.
        plugin:
            providers: [command:demo]
            source: github.com/opencharly/charly/candy/plugin-demo
        plan:
            - check: /bin/true
              command: "true"
`
	if err := os.WriteFile(filepath.Join(dir, "candy", "plugin-demo", "charly.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	roots, err := RepoRoots(dir)
	if err != nil {
		t.Fatal(err)
	}
	ents, err := CollectEntities(roots)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("CollectEntities = %d entities, want 1; %+v", len(ents), ents)
	}
	e := ents[0]
	if e.Name != "plugin-demo" || e.Kind != "candy" {
		t.Fatalf("entity = %s/%s, want plugin-demo/candy", e.Name, e.Kind)
	}
	// The nested `plugin:` sub-mapping must decode (the trap the pointer-node form hit).
	var view struct {
		Version string `yaml:"version"`
		Plugin  struct {
			Providers []string `yaml:"providers"`
		} `yaml:"plugin"`
	}
	if err := e.Value.Decode(&view); err != nil {
		t.Fatalf("decode discovered value: %v", err)
	}
	if view.Version == "" || len(view.Plugin.Providers) != 1 || view.Plugin.Providers[0] != "command:demo" {
		t.Fatalf("nested plugin block not round-tripped: %+v", view)
	}
}

// TestReadEntityFile_SkipsNonCatalogShapes proves a file that does not fit the name-first shape
// (a scalar top-level key like a directive, or a multi-doc template) is skipped, not failed.
func TestReadEntityFile_SkipsNonCatalogShapes(t *testing.T) {
	dir := t.TempDir()
	body := "version: 2026.218.1200\nproviders: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "charly.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, err := ReadEntityFile(filepath.Join(dir, "charly.yml"), Root{Dir: dir}, ".")
	if err != nil {
		t.Fatalf("ReadEntityFile on a directive-only file must not fail: %v", err)
	}
	if len(ents) != 0 {
		t.Fatalf("directive-only file produced %d entities, want 0", len(ents))
	}
}
