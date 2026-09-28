package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCollectRemoteRefsOpts_VersionlessRefResolvesDefaultBranch pins the corrected
// contract: a version-less remote candy ref (`@github.com/owner/repo`, no `:tag`) means
// "the current tip of that repo", so it resolves to the repo's DEFAULT BRANCH — not a
// frozen latest tag. The latest-tag behavior silently served a STALE build whenever the
// repo had newer work on its default branch than on its last tag, and SHADOWED a
// project's explicit pin for the same word (opencharly/charly#715). The branch resolver
// is a package seam, so the test is hermetic — no live git ls-remote at test time.
func TestCollectRemoteRefsOpts_VersionlessRefResolvesDefaultBranch(t *testing.T) {
	cfg := &spec.Config{}
	layers := map[string]spec.CandyReader{}
	opts := spec.ResolveOpts{ExtraCandyRefs: []string{"@github.com/opencharly/layer-ripgrep"}}
	// A stub downloader (the branch resolution happens BEFORE any download) + a stubbed
	// default-branch resolver (no live git ls-remote — the test is offline-safe and deterministic).
	seams := spec.RefsCollectSeams{Downloader: fakeDownloader{}}
	orig := resolveDefaultBranch
	resolveDefaultBranch = func(repoURL string) (string, error) {
		if repoURL != "https://github.com/opencharly/layer-ripgrep.git" {
			t.Fatalf("resolveDefaultBranch called with %q; want the layer-ripgrep repo URL", repoURL)
		}
		return "main", nil
	}
	t.Cleanup(func() { resolveDefaultBranch = orig })
	downloads, err := CollectRemoteRefsOpts(cfg, layers, opts, seams)
	if err != nil {
		t.Fatalf("CollectRemoteRefsOpts: %v", err)
	}
	found := false
	for _, d := range downloads {
		if d.RepoPath == "github.com/opencharly/layer-ripgrep" {
			found = true
			if d.Version != "main" {
				t.Fatalf("version-less ref resolved to %q; want the default branch main", d.Version)
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
