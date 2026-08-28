package loaderkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// parse_candy_manifest_fallback_test.go — the "charly.yml is everything at once"
// fallback: ParseCandyManifest must find a candy node in a PROJECT file (version/
// repo/import/discover + entity nodes) even when ParseDoc fails — the empty-Threaded
// resolver context before the provider registry is loaded.

func TestParseCandyManifest_ProjectFileFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "charly.yml")
	body := `version: 2026.232.0520
repo: github.com/opencharly/example
discover:
    - path: candy
      recursive: true
check-some-bed:
    pod:
        image: example
        disposable: true
example-candy:
    candy:
        version: 2026.232.0001
        description: a candy node in a project file
        package:
            - example
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Empty Threaded — ParseDoc fails, the fallback must find the candy node.
	ly, err := ParseCandyManifest(path, spec.Threaded{}, spec.CandyVocab{})
	if err != nil {
		t.Fatalf("ParseCandyManifest: %v", err)
	}
	if ly.Name != "example-candy" {
		t.Errorf("name = %q, want example-candy", ly.Name)
	}
	if len(ly.Package) == 0 || ly.Package[0].Name != "example" {
		t.Errorf("package = %v, want [example]", ly.Package)
	}
}
