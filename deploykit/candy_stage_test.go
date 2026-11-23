package deploykit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// TestCandyCopySource covers CandyCopySource's local-vs-remote build-context path
// resolution, which lives in this package as real production code (used by EmitScratchStages,
// GenerateInitFragments's stage_fragment_copy rendering, and the pod-overlay
// build path).
func TestCandyCopySource(t *testing.T) {
	g := &Generator{
		Candies: map[string]CandyModel{
			"pixi": NewSpecCandyModel(spec.CandyModel{Name: "pixi"}, spec.CandyView{Remote: false}),
			"github.com/test/repo/layers/cuda": NewSpecCandyModel(
				spec.CandyModel{Name: "cuda", Version: "2026.167.1200"},
				spec.CandyView{Remote: true, RepoPath: "github.com/test/repo"},
			),
			"github.com/test/repo/layers/nover": NewSpecCandyModel(
				spec.CandyModel{Name: "nover"},
				spec.CandyView{Remote: true, RepoPath: "github.com/test/repo"},
			),
		},
	}

	if got := g.CandyCopySource("pixi"); got != "candy/pixi" {
		t.Errorf("local candy: got %q, want %q", got, "candy/pixi")
	}
	// Remote candies stage under the VERSION-keyed .build/_candy/<name>.<version>/
	// so distinct candy versions never share a dir (cross-version crossover).
	if got := g.CandyCopySource("github.com/test/repo/layers/cuda"); got != ".build/_candy/cuda.2026.167.1200" {
		t.Errorf("remote candy: got %q, want %q", got, ".build/_candy/cuda.2026.167.1200")
	}
	// Defensive: a version-less remote candy (should never happen — the resolver
	// hard-errors) falls back to the bare name rather than a trailing-dot dir.
	if got := g.CandyCopySource("github.com/test/repo/layers/nover"); got != ".build/_candy/nover" {
		t.Errorf("version-less remote candy: got %q, want %q", got, ".build/_candy/nover")
	}
}

// TestCandyCopySource_ImportedNamespaceOrigin is the renderer-side guard for
// opencharly/.github#263: a candy DECLARED IN an imported namespace lives in that import's
// own fetched checkout, OUTSIDE the importing project's tree. The build context is the
// project, so no project-relative candy/<name>/ path can name it — the copy source must be
// the staged .build/_candy/<name>.<version>/ that createRemoteCandyCopies produces. The
// origin is decided from WHERE the candy lives (plus the Remote marker), never from its
// name alone.
func TestCandyCopySource_ImportedNamespaceOrigin(t *testing.T) {
	project := t.TempDir()
	// A fetched namespace checkout beside (never under) the importing project, the shape
	// the resolver materializes an `import: [{cachyos: '@github…'}]` mount into.
	namespaceCheckout := t.TempDir()
	candyDir := filepath.Join(namespaceCheckout, "candy", "cachyos-base-check")
	if err := os.MkdirAll(candyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := &Generator{
		Dir: project,
		Candies: map[string]CandyModel{
			// NOT Remote: this is the pre-fix namespace-resolved state, which is exactly why
			// the copy source must be derived from the resolved directory, not the flag alone.
			"cachyos-base-check": NewSpecCandyModel(
				spec.CandyModel{Name: "cachyos-base-check", Version: "2026.300.1129", SourceDir: candyDir},
				spec.CandyView{Name: "cachyos-base-check"},
			),
			// The two origins that already worked must keep working: a project-local candy at
			// the default location, and an inside-the-project candy anchored elsewhere.
			"pixi": NewSpecCandyModel(
				spec.CandyModel{Name: "pixi", SourceDir: filepath.Join(project, "candy", "pixi")},
				spec.CandyView{Name: "pixi"},
			),
			"anchored": NewSpecCandyModel(
				spec.CandyModel{Name: "anchored", SourceDir: filepath.Join(project, "layers", "anchored")},
				spec.CandyView{Name: "anchored"},
			),
			"github.com/test/repo/layers/cuda": NewSpecCandyModel(
				spec.CandyModel{Name: "cuda", Version: "2026.167.1200"},
				spec.CandyView{Name: "cuda", Remote: true, RepoPath: "github.com/test/repo"},
			),
		},
	}

	cases := []struct{ ref, want string }{
		{"cachyos-base-check", ".build/_candy/cachyos-base-check.2026.300.1129"},
		{"pixi", "candy/pixi"},
		{"anchored", "layers/anchored"},
		{"github.com/test/repo/layers/cuda", ".build/_candy/cuda.2026.167.1200"},
	}
	for _, tc := range cases {
		if got := g.CandyCopySource(tc.ref); got != tc.want {
			t.Errorf("CandyCopySource(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

// TestEmitScratchStage_NeverStagesTheBuildRoot is the regression guard for opencharly/sdk#623:
// an INLINE candy (declared in the project's root charly.yml) has the build root as its own
// directory, and staging it as a whole would put the entire project — .git, .check run logs,
// .build staging, any local secret — into the image's build graph, with the context-ignore
// DENYLIST as its only protection. And a candy nothing copies from gets no stage at all.
func TestEmitScratchStage_NeverStagesTheBuildRoot(t *testing.T) {
	root := t.TempDir()
	g := &Generator{
		Dir: root,
		Candies: map[string]CandyModel{
			"inline-probe": NewSpecCandyModel(spec.CandyModel{
				Name:      "inline-probe",
				SourceDir: root,
				RunOps:    []vmshared.Op{{Copy: "etc/probe.conf", To: "/etc/probe.conf"}},
			}, spec.CandyView{}),
			"idle": NewSpecCandyModel(spec.CandyModel{
				Name:      "idle",
				SourceDir: filepath.Join(root, "candy", "idle"),
			}, spec.CandyView{}),
			"pixi": NewSpecCandyModel(spec.CandyModel{
				Name:      "pixi",
				SourceDir: filepath.Join(root, "candy", "pixi"),
				RunOps:    []vmshared.Op{{Copy: "etc/x", To: "/etc/x"}},
			}, spec.CandyView{}),
		},
	}

	var b strings.Builder
	g.EmitScratchStages(&b, []string{"inline-probe", "idle", "pixi"})
	out := b.String()

	if strings.Contains(out, "COPY ./ /") {
		t.Fatalf("the generator staged the whole build root:\n%s", out)
	}
	if !strings.Contains(out, "FROM scratch AS inline-probe\nCOPY etc/probe.conf etc/probe.conf\n") {
		t.Errorf("an inline candy must be staged by the sources its plan reads, got:\n%s", out)
	}
	if strings.Contains(out, "FROM scratch AS idle") {
		t.Errorf("a candy nothing copies from must get no stage, got:\n%s", out)
	}
	if !strings.Contains(out, "FROM scratch AS pixi\nCOPY candy/pixi/ /\n") {
		t.Errorf("a conventional candy keeps its directory stage, got:\n%s", out)
	}
}
