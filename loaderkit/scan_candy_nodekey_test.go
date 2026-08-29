package loaderkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestScanRepoCandyDir_NodeKeyNameOverride proves the ACTUAL candy name is the
// manifest's node key (ly.Name), not the repo-path-derived dir name: a root-level
// standalone candy repo whose manifest node key differs from its directory name (e.g.
// repo layer-versatiles-style owning the candy `versatiles-style`) must be keyed by
// the node key so bare-name refs (`versatiles-style` in a sibling's list) resolve.
func TestScanRepoCandyDir_NodeKeyNameOverride(t *testing.T) {
	repoDir := t.TempDir()
	candyDir := filepath.Join(repoDir, "candy", "layer-versatiles-style")
	if err := os.MkdirAll(candyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `versatiles-style:
    candy:
        version: 2026.232.0520
        description: style bundle
`
	if err := os.WriteFile(filepath.Join(candyDir, spec.UnifiedFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	parseDoc := func(path string) (*spec.CandyYAML, error) {
		return ParseCandyManifest(path, spec.Threaded{Kinds: map[string]bool{"candy": true}}, spec.CandyVocab{})
	}

	candies, err := scanRepoCandyDir(repoDir, "github.com/opencharly/layer-versatiles-style", parseDoc)
	if err != nil {
		t.Fatalf("scanRepoCandyDir: %v", err)
	}

	// The map key is the dir name (the repo-path-derived name), but the MODEL name
	// must be the manifest node key (versatiles-style) — the scan_orchestrate
	// bare-name keying uses Model.Name, so a bare-name ref to `versatiles-style`
	// resolves even though the @github path key carries the dir name.
	sc, ok := candies["github.com/opencharly/layer-versatiles-style/candy/layer-versatiles-style"]
	if !ok {
		got := make([]string, 0, len(candies))
		for k := range candies {
			got = append(got, k)
		}
		t.Fatalf("candy not found by dir-name key; got keys %v", got)
	}
	if sc.Model.Name != "versatiles-style" {
		t.Fatalf("Model.Name = %q, want %q (the manifest node key)", sc.Model.Name, "versatiles-style")
	}
}
