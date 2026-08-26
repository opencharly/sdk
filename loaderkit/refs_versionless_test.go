package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCollectRemoteRefsOpts_VersionlessRefResolvesLatestTag pins the Phase-4 behavior: a
// version-less remote candy ref (e.g. a builder plugin connected by word ref) resolves to the
// repo's LATEST TAG — immutable — not the mutable default branch (which a freshness-checked
// fetch would hang on in a network-bound CI/container).
func TestCollectRemoteRefsOpts_VersionlessRefResolvesLatestTag(t *testing.T) {
	cfg := &spec.Config{}
	layers := map[string]spec.CandyReader{}
	opts := spec.ResolveOpts{ExtraCandyRefs: []string{"@github.com/opencharly/layer-ripgrep"}}
	// A stub downloader (the tag resolution happens BEFORE any download).
	seams := spec.RefsCollectSeams{Downloader: fakeDownloader{}}
	downloads, err := CollectRemoteRefsOpts(cfg, layers, opts, seams)
	if err != nil {
		t.Fatalf("CollectRemoteRefsOpts: %v", err)
	}
	found := false
	for _, d := range downloads {
		if d.RepoPath == "github.com/opencharly/layer-ripgrep" {
			found = true
			if d.Version == "main" {
				t.Fatalf("version-less ref resolved to mutable main; want a v<calver> tag")
			}
			t.Logf("version-less ref resolved to %s", d.Version)
		}
	}
	if !found {
		t.Fatalf("layer-ripgrep download not collected; got %+v", downloads)
	}
}

// fakeDownloader is a no-op RefsDownloader for the versionless-ref test.
type fakeDownloader struct{}

func (fakeDownloader) Download(repoPath, version string) (string, error) { return "", nil }
