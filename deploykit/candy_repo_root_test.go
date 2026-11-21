package deploykit

import (
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// candy_repo_root_test.go — candyRepoRoot, the ONE derivation of "the root of the repo that authors
// this candy", which every owned-content request carries as a candy's repo_root (the base a kind
// resolves a derived body against). These cases were covered through the label collector before it
// moved to kind:skill (opencharly/sdk#598); the derivation stayed here, so its coverage does too —
// each of the loader's SourceDir shapes, and the disagreement it must refuse rather than paper over.
func TestCandyRepoRoot_EveryLoaderShape(t *testing.T) {
	repo := t.TempDir()
	cases := []struct {
		name   string
		model  spec.CandyModel
		view   spec.CandyView
		want   string
		wantOK bool
	}{
		{
			// A LOCAL candy at the project loader's `candy/<name>` placement: the suffix is recovered
			// structurally — the body at `candy/<name>/.agents` is NOT a home (sdk#434's 48 bodies).
			name:  "local candy under candy/<name>",
			model: spec.CandyModel{Name: "plugin-review", SourceDir: filepath.Join(repo, "candy", "plugin-review")},
			view:  spec.CandyView{Name: "plugin-review"},
			want:  repo, wantOK: true,
		},
		{
			// A ROOT-LEVEL standalone candy (the de-submodule cutover's shape): its dir IS the repo root.
			name:  "root-level standalone candy",
			model: spec.CandyModel{Name: "review-standalone", SourceDir: repo},
			view:  spec.CandyView{Name: "review-standalone", Remote: true, RepoPath: "github.com/opencharly/review-standalone"},
			want:  repo, wantOK: true,
		},
		{
			// A REMOTE candy of a multi-candy repo: the loader recorded the in-repo subpath, which is
			// stripped.
			name:  "remote subpath candy",
			model: spec.CandyModel{Name: "arch-test", SourceDir: filepath.Join(repo, "candy", "arch-test")},
			view:  spec.CandyView{Name: "arch-test", Remote: true, RepoPath: "github.com/opencharly/distro-arch", SubPathPrefix: "candy/"},
			want:  repo, wantOK: true,
		},
		{
			// The recorded subpath disagrees with the on-disk path: a finding, never the candy dir.
			name:  "subpath that does not match the source dir",
			model: spec.CandyModel{Name: "arch-test", SourceDir: filepath.Join(repo, "elsewhere", "arch-test")},
			view:  spec.CandyView{Name: "arch-test", Remote: true, SubPathPrefix: "candy/"},
			want:  "", wantOK: false,
		},
		{
			name:  "no source dir",
			model: spec.CandyModel{Name: "ghost"},
			view:  spec.CandyView{Name: "ghost"},
			want:  "", wantOK: false,
		},
	}
	for _, c := range cases {
		got, ok := candyRepoRoot(NewSpecCandyModel(c.model, c.view))
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: candyRepoRoot = (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}
