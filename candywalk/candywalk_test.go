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

// TestCollectEntitiesRemote_FetchesRemoteCandy is the Phase-3 discovery seam proof: a local root
// whose candy dir references a MOVED candy via @github.com/opencharly/... resolves that repo
// through the caller-supplied resolver and includes the fetched candy's entity (root-manifest
// layout, SourceRoot = the fetched dir) in the union. This is exactly what Phase 4 requires:
// after the in-repo candy/ dirs are deleted, discovery still surfaces every moved candy.
func TestCollectEntitiesRemote_FetchesRemoteCandy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "candy", "local-keeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := `local-keeper:
    candy:
        version: 2026.200.1000
        description: A local fixture candy.
`
	if err := os.WriteFile(filepath.Join(root, "candy", "local-keeper", "charly.yml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	// The fetched "remote repo" is a synthetic export dir: a config candy's root manifest.
	fetched := t.TempDir()
	remoteManifest := `ripgrep:
    candy:
        version: 2026.144.1443
        description: Fast recursive text search.
`
	if err := os.WriteFile(filepath.Join(fetched, "charly.yml"), []byte(remoteManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	roots := []Root{{Namespace: "", Dir: root}}
	resolve := func(repoPath, version string) (string, error) {
		if repoPath != "github.com/opencharly/layer-ripgrep" || version != "v2026.144.1443" {
			t.Fatalf("resolver called with unexpected repo %q version %q", repoPath, version)
		}
		return fetched, nil
	}

	// The local keeper references the moved ripgrep candy in its candy: list.
	ref := "@github.com/opencharly/layer-ripgrep:v2026.144.1443"
	if err := os.WriteFile(filepath.Join(root, "candy", "local-keeper", "charly.yml"),
		[]byte(local+"        candy:\n            - '"+ref+"'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ents, err := CollectEntitiesRemote(roots, resolve)
	if err != nil {
		t.Fatalf("CollectEntitiesRemote: %v", err)
	}
	var remote *Entity
	for i := range ents {
		if ents[i].Name == "ripgrep" {
			remote = &ents[i]
		}
	}
	if remote == nil {
		t.Fatalf("moved candy ripgrep not discovered from the remote; got entities: %+v", ents)
	}
	if remote.SourceRoot != fetched {
		t.Fatalf("remote entity SourceRoot = %q, want %q (schema reads must resolve against the fetched tree)", remote.SourceRoot, fetched)
	}
	if remote.Dir != "." {
		t.Fatalf("remote root-manifest entity Dir = %q, want \".\"", remote.Dir)
	}
}

// TestCollectEntities_UnchangedDefault pins the pre-Phase-4 default: CollectEntities (no resolver)
// is byte-for-byte the pure local FS walk — no remote resolution, no SourceRoot override.
func TestCollectEntities_UnchangedDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "candy", "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `plain:
    candy:
        version: 2026.200.1000
        description: A local fixture candy.
`
	if err := os.WriteFile(filepath.Join(dir, "candy", "plain", "charly.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, err := CollectEntities([]Root{{Namespace: "", Dir: dir}})
	if err != nil {
		t.Fatalf("CollectEntities: %v", err)
	}
	if len(ents) != 1 || ents[0].Name != "plain" {
		t.Fatalf("local walk: got %+v, want exactly the plain entity", ents)
	}
	if ents[0].SourceRoot != dir {
		t.Fatalf("local entity SourceRoot = %q, want the local root %q", ents[0].SourceRoot, dir)
	}
}
