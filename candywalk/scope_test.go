package candywalk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCollectEntitiesRemoteScoped_NarrowsOneFile proves the seam: a scope can keep an owner
// candy's refs and drop a fixture's, while the full-closure walker is untouched (its own tests
// run with a nil scope).
func TestCollectEntitiesRemoteScoped_NarrowsOneFile(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("candy/source/charly.yml", `source:
  candy:
    require:
      - '@github.com/opencharly/needed:v2026.100.0000'
    candy:
      - '@github.com/opencharly/also-needed:v2026.100.0000'
source-skill:
  skill:
    name: source
    family: test
    owner: source
`)
	write("box/fixture/charly.yml", `fixture:
  candy:
    base: fedora
    candy:
      - '@github.com/opencharly/retired:v2026.100.0000'
`)

	// ownerScope mirrors the corpus rule: a candy-file contributes all its refs only when its
	// candy owns a skill/hook/marketplace kind.
	ownerScope := func(_ string, candyName string, ents []Entity, all []string) []string {
		if candyName == "" {
			return all
		}
		for _, e := range ents {
			switch e.Kind {
			case "hook", "marketplace":
				return all
			case "skill":
				var s struct {
					Owner string `yaml:"owner"`
				}
				if e.Value.Decode(&s) == nil && s.Owner == candyName {
					return all
				}
			}
		}
		return nil
	}

	var asked []string
	resolve := func(repoPath, version string) (string, error) {
		asked = append(asked, repoPath+"@"+version)
		return t.TempDir(), nil
	}
	if _, err := CollectEntitiesRemoteScoped([]Root{{Dir: dir}}, resolve, ownerScope); err != nil {
		t.Fatalf("CollectEntitiesRemoteScoped: %v", err)
	}
	joined := strings.Join(asked, " ")
	for _, want := range []string{"needed@", "also-needed@"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("owner ref %q was not followed: %v", want, asked)
		}
	}
	if strings.Contains(joined, "retired@") {
		t.Fatalf("fixture ref was followed into the corpus: %v", asked)
	}
}
