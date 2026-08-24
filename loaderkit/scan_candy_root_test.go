package loaderkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// scan_candy_root_test.go — the root-level remote candy (the candy de-submodule
// cutover): a standalone candy repo's manifest lives at the repo ROOT, so the
// bare ref's last path segment is the repo itself. ScanRemoteCandy must derive
// the candy name from the repo's own name when the sub-path is empty, and
// QualifyRemoteSiblingDeps must NOT fabricate sibling paths for a root-level
// remote (it has no siblings).
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

// The root-level remote candy has NO siblings: a bare dep must NOT be qualified
// to a fabricated sibling path (the candy de-submodule cutover).
func TestQualifyRemoteSiblingDeps_RootLevelSkips(t *testing.T) {
	refs := spec.CandyRefs{
		Require:       []spec.CandyRefEntry{{Raw: "pixi"}},
		IncludedCandy: []spec.CandyRefEntry{{Raw: "ffmpeg"}},
		BakePlugin:    []spec.CandyRefEntry{{Raw: "plugin-x"}},
	}
	QualifyRemoteSiblingDeps("github.com/opencharly/layer-python", "", &refs)
	for _, e := range refs.Require {
		if e.Resolved != "" {
			t.Fatalf("root-level require qualified to %q — must stay bare", e.Resolved)
		}
	}
	for _, e := range refs.IncludedCandy {
		if e.Resolved != "" {
			t.Fatalf("root-level candy dep qualified to %q — must stay bare", e.Resolved)
		}
	}
	for _, e := range refs.BakePlugin {
		if e.Resolved != "" {
			t.Fatalf("root-level bake_plugin qualified to %q — must stay bare", e.Resolved)
		}
	}
}

// The candy-library form (SubPathPrefix "candy/") must keep qualifying bare
// deps to siblings in the same repo.
func TestQualifyRemoteSiblingDeps_SubPathStillQualifies(t *testing.T) {
	refs := spec.CandyRefs{Require: []spec.CandyRefEntry{{Raw: "ripgrep"}}}
	QualifyRemoteSiblingDeps("github.com/opencharly/charly", "candy/", &refs)
	if refs.Require[0].Resolved != "github.com/opencharly/charly/candy/ripgrep" {
		t.Fatalf("sub-path dep = %q, want the sibling qualification", refs.Require[0].Resolved)
	}
}

// scan_orchestrate_test.go's sibling — the FETCH fix-point's mirror of the
// qualify skip. A root-level remote candy (SubPathPrefix "") has NO siblings,
// so the enqueue leg must NOT fabricate repoPath+"/"+dep for a bare dep: the
// next fix-point round would scan the fabricated ref against the repo at the
// tag and die "remote candy github.com/opencharly/layer-cuda/nvidia not found
// at .../layer-cuda@v2026.235.2110/nvidia" (the exact batch-1 bed failure).
// The bare dep must instead be left to resolve against the scan set (the local
// library or another downloaded remote).
func TestScanCandyFromLocal_RootLevelBareDepNotEnqueued(t *testing.T) {
	const (
		repoPath  = "github.com/opencharly/layer-cuda"
		tag       = "v2026.235.2110"
		rootRef   = "github.com/opencharly/layer-cuda"
		localName = "nvidia"
	)
	repoDir := t.TempDir()
	body := `version: 2026.232.0520
cuda:
    candy:
        version: 2026.229.1218
        require:
            - nvidia
            - '@github.com/opencharly/layer-ffmpeg:v2026.235.2057'
`
	if err := os.WriteFile(filepath.Join(repoDir, "charly.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	parseDoc := func(path string) (*spec.CandyYAML, error) {
		return ParseCandyManifest(path, spec.Threaded{Kinds: map[string]bool{"candy": true}}, spec.CandyVocab{})
	}

	// ScanRemote fails the test if it is ever asked for the FABRICATED sibling
	// ref — that is the regression this test gates. The only refs the fix-point
	// may scan are the root ref itself and the @-pinned remote dep.
	var scannedRefs []string
	ffmpegDir := t.TempDir()
	ffmpegBody := `version: 2026.232.0520
ffmpeg:
    candy:
        version: 2026.235.2057
        description: ffmpeg
        package:
            - ffmpeg
`
	if err := os.WriteFile(filepath.Join(ffmpegDir, "charly.yml"), []byte(ffmpegBody), 0o644); err != nil {
		t.Fatal(err)
	}
	seams := spec.ScanSeams{
		CollectRemoteRefs: func(map[string]spec.ScannedCandy) ([]spec.RemoteDownload, error) {
			return []spec.RemoteDownload{{RepoPath: repoPath, Version: tag, Refs: []string{rootRef}}}, nil
		},
		EnsureRepo: func(repo, _ string) (string, error) {
			if repo == "github.com/opencharly/layer-ffmpeg" {
				return ffmpegDir, nil
			}
			return repoDir, nil
		},
		ScanRemote: func(cacheDir, repo string, wantRefs map[string]bool) (map[string]spec.ScannedCandy, error) {
			for ref := range wantRefs {
				scannedRefs = append(scannedRefs, ref)
				if ref != rootRef && ref != "github.com/opencharly/layer-ffmpeg" {
					return nil, fmt.Errorf("fix-point scanned fabricated ref %q — the root-level bare dep must not be enqueued as a sibling", ref)
				}
			}
			return ScanRemoteCandy(cacheDir, repo, wantRefs, parseDoc)
		},
	}

	// The bare `nvidia` dep must resolve against the SCAN SET — the local
	// library — not a fabricated sibling in the layer-cuda repo.
	local := map[string]spec.ScannedCandy{
		localName: {
			Model: spec.CandyModel{Name: localName, Version: "2026.229.1218", SourceDir: "/local/candy/" + localName},
			View:  spec.CandyView{Name: localName},
		},
	}

	got, err := ScanCandyFromLocal(local, nil, seams)
	if err != nil {
		t.Fatalf("ScanCandyFromLocal: %v", err)
	}
	root, ok := got[rootRef]
	if !ok {
		t.Fatalf("root-level ref %q missing from scan result; got %v", rootRef, got)
	}
	if root.GetSourceDir() != repoDir {
		t.Fatalf("root-level SourceDir = %q, want %q", root.GetSourceDir(), repoDir)
	}
	// The local dep must still be present under its bare name.
	if _, ok := got[localName]; !ok {
		t.Fatalf("local dep %q missing from scan result", localName)
	}
	for _, ref := range scannedRefs {
		if ref == repoPath+"/nvidia" {
			t.Fatalf("fix-point scanned the fabricated sibling ref %q", ref)
		}
	}
}

