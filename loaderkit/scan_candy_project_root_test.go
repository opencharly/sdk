package loaderkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// scan_candy_project_root_test.go — the "charly.yml is everything at once" contract:
// a repo-root @github ref to a PROJECT repo (whose root charly.yml is a project file,
// not a candy manifest) must provide EVERY candy the repo owns via its candy/ dir.
// Before this fix, ScanRemoteCandy tried to parse the project root as a single candy
// and failed with "unrecognized candy manifest shape".

func TestScanRemoteCandy_ProjectRepoRootProvidesCandyDir(t *testing.T) {
	repoDir := t.TempDir()
	// A project repo: root charly.yml is a project file (version + entity nodes),
	// candies live in candy/ subdirectories.
	root := `version: 2026.232.0520
repo: github.com/opencharly/example
discover:
    - path: candy
      recursive: true
check-some-bed:
    pod:
        image: example
        disposable: true
`
	if err := os.WriteFile(filepath.Join(repoDir, "charly.yml"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	candyDir := filepath.Join(repoDir, "candy", "example-candy")
	if err := os.MkdirAll(candyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	candy := `example-candy:
    candy:
        version: 2026.232.0001
        description: a candy in the project repo's candy dir
        package:
            - example
`
	if err := os.WriteFile(filepath.Join(candyDir, "charly.yml"), []byte(candy), 0o644); err != nil {
		t.Fatal(err)
	}

	repoPath := "github.com/opencharly/example"
	wantRefs := map[string]bool{repoPath: true}
	got, err := ScanRemoteCandy(repoDir, repoPath, wantRefs, func(path string) (*spec.CandyYAML, error) {
		return ParseCandyManifest(path, spec.Threaded{Kinds: map[string]bool{"candy": true}}, spec.CandyVocab{})
	})
	if err != nil {
		t.Fatalf("ScanRemoteCandy: %v", err)
	}
	// The repo-root ref must provide the candy/ dir candy, keyed by its bare ref.
	bare := repoPath + "/candy/example-candy"
	sc, ok := got[bare]
	if !ok {
		t.Fatalf("expected candy %q in result, got keys: %v", bare, keysOf(got))
	}
	if sc.View.Name != "example-candy" {
		t.Errorf("candy name = %q, want example-candy", sc.View.Name)
	}
	if !sc.View.Remote || sc.View.RepoPath != repoPath {
		t.Errorf("candy not marked remote with repo path %q: remote=%v repoPath=%q", repoPath, sc.View.Remote, sc.View.RepoPath)
	}
}

func keysOf(m map[string]spec.ScannedCandy) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
