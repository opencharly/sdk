package loaderkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// scan_candy_root_test.go — the root-level remote candy (the candy de-submodule
// cutover): a standalone candy repo's manifest lives at the repo ROOT, so the
// bare ref's last path segment is the repo itself. ScanRemoteCandy must derive
// the candy name from the repo's own name when the sub-path is empty.

func TestScanRemoteCandy_RootLevelNameDerivation(t *testing.T) {
	repoDir := t.TempDir()
	body := `version: 2026.232.0520
ripgrep:
    candy:
        version: 2026.144.1443
        description: Fast recursive text search (rg)
        package:
            - ripgrep
`
	if err := os.WriteFile(filepath.Join(repoDir, "charly.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	parseDoc := func(path string) (*spec.CandyYAML, error) {
		return ParseCandyManifest(path, spec.Threaded{Kinds: map[string]bool{"candy": true}}, spec.CandyVocab{})
	}

	// Root-level ref: the bare ref IS the repo path (no /candy/<name> sub-path).
	got, err := ScanRemoteCandy(repoDir, "github.com/opencharly/ripgrep", map[string]bool{"github.com/opencharly/ripgrep": true}, parseDoc)
	if err != nil {
		t.Fatalf("ScanRemoteCandy: %v", err)
	}
	sc, ok := got["github.com/opencharly/ripgrep"]
	if !ok {
		t.Fatalf("scanned map missing the root-level ref; got %v", got)
	}
	if sc.Model.Name != "ripgrep" {
		t.Fatalf("name = %q, want %q (derived from the repo's own name)", sc.Model.Name, "ripgrep")
	}
	if sc.View.Name != "ripgrep" {
		t.Fatalf("view name = %q, want %q", sc.View.Name, "ripgrep")
	}
	if !sc.View.Remote {
		t.Fatal("view not marked remote")
	}
	if sc.View.RepoPath != "github.com/opencharly/ripgrep" {
		t.Fatalf("repo path = %q", sc.View.RepoPath)
	}
}

// The sub-path form (the pre-cutover shape — a candy library inside a repo) must
// keep deriving the name from the ref's last segment.
func TestScanRemoteCandy_SubPathNameUnchanged(t *testing.T) {
	repoDir := t.TempDir()
	body := `ripgrep:
    candy:
        version: 2026.144.1443
        description: Fast recursive text search (rg)
`
	if err := os.MkdirAll(filepath.Join(repoDir, "candy", "ripgrep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "candy", "ripgrep", "charly.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	parseDoc := func(path string) (*spec.CandyYAML, error) {
		return ParseCandyManifest(path, spec.Threaded{Kinds: map[string]bool{"candy": true}}, spec.CandyVocab{})
	}

	got, err := ScanRemoteCandy(repoDir, "github.com/opencharly/charly", map[string]bool{"github.com/opencharly/charly/candy/ripgrep": true}, parseDoc)
	if err != nil {
		t.Fatalf("ScanRemoteCandy: %v", err)
	}
	sc := got["github.com/opencharly/charly/candy/ripgrep"]
	if sc.Model.Name != "ripgrep" {
		t.Fatalf("name = %q, want %q (last segment of the sub-path)", sc.Model.Name, "ripgrep")
	}
	if sc.View.SubPathPrefix != "candy/" {
		t.Fatalf("sub-path prefix = %q, want %q", sc.View.SubPathPrefix, "candy/")
	}
}

// A ref NOT under repoPath at all (a typo'd repo path) must keep the hard
// "not found" error — the root-level guard fires only on the exact repo path.
func TestScanRemoteCandy_ForeignRefStillErrors(t *testing.T) {
	repoDir := t.TempDir()
	body := `version: 2026.232.0520
ripgrep:
    candy:
        version: 2026.144.1443
        description: Fast recursive text search (rg)
`
	if err := os.WriteFile(filepath.Join(repoDir, "charly.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	parseDoc := func(path string) (*spec.CandyYAML, error) {
		return ParseCandyManifest(path, spec.Threaded{Kinds: map[string]bool{"candy": true}}, spec.CandyVocab{})
	}

	// A typo'd repo path (charlyy) is NOT the repo itself and NOT under it: the
	// scan must fail loudly, never silently scan this repo's root.
	_, err := ScanRemoteCandy(repoDir, "github.com/opencharly/ripgrep", map[string]bool{"github.com/opencharly/charlyy/candy/ripgrep": true}, parseDoc)
	if err == nil {
		t.Fatal("foreign ref scanned without error — the root-level guard must fire only on the exact repo path")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("foreign ref error = %v, want the hard not-found error", err)
	}
}
