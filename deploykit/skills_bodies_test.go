package deploykit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// skills_bodies_test.go — CollectSkillBodies must resolve a body for EVERY authored node, with no box
// composition involved. Both controls, and they are the ones opencharly/opencharly#557 flipped: the
// same tree now PASSES with the body at the repo root and FAILS with the body beside the candy,
// because the repo root is the base the marketplace corpus reader resolves against. The pre-change
// code path (CollectSkills) cannot see either, because BoxCandyChain is empty for a repo that
// composes no box — which is the defect in opencharly/opencharly#530.

// skillBodiesFixture builds a project with ONE candy at `candy/plugin-review/` in a repo whose root
// is dir, and the skill body at exactly one of the two candidate locations.
func skillBodiesFixture(t *testing.T, atRoot bool) (*spec.Config, map[string]CandyModel) {
	t.Helper()
	dir := t.TempDir()
	tree := filepath.Join(dir, ".agents", "skills", "review")
	if !atRoot {
		tree = filepath.Join(dir, "candy", "plugin-review", ".agents", "skills", "review")
	}
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: review\ndescription: Review a PR\n---\n\nReviews things.\n"
	if err := os.WriteFile(filepath.Join(tree, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(dir, "candy", "plugin-review")
	layers := map[string]CandyModel{
		"plugin-review": NewSpecCandyModel(
			spec.CandyModel{Name: "plugin-review", SourceDir: srcDir},
			spec.CandyView{Name: "plugin-review", Description: "review"},
		),
	}
	cfg := &spec.Config{
		Skills: map[string]json.RawMessage{
			"review": json.RawMessage(`{"owner":"plugin-review","family":"charly-internals"}`),
		},
	}
	return cfg, layers
}

// TestCollectSkillBodies_AtTheRepoRoot is the POSITIVE control: a candy at `candy/<name>/` whose body
// sits at the repo root must resolve, and the prose must come back from that file.
func TestCollectSkillBodies_AtTheRepoRoot(t *testing.T) {
	cfg, layers := skillBodiesFixture(t, true)
	got, err := CollectSkillBodies(cfg, layers)
	if err != nil {
		t.Fatalf("a body at the repo root of the owning candy must resolve: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if !strings.Contains(got[0].Content, "Reviews things.") {
		t.Fatalf("content not read back from the file: %q", got[0].Content)
	}
}

// TestCollectSkillBodies_BesideTheCandyIsNotAHome is the NEGATIVE control, and it is the half that
// makes the positive one mean something: the candy-dir layout that opencharly/sdk#434's re-home
// produced must be REFUSED, because the corpus reader cannot reach it. If this test ever resolves,
// the two bases have drifted apart again.
func TestCollectSkillBodies_BesideTheCandyIsNotAHome(t *testing.T) {
	cfg, layers := skillBodiesFixture(t, false)
	_, err := CollectSkillBodies(cfg, layers)
	if err == nil {
		t.Fatal("a body beside the candy must be an error — the one base is the repo root, not the candy dir")
	}
	if !strings.Contains(err.Error(), ".agents/skills") {
		t.Fatalf("the finding must name the tree it looked in: %v", err)
	}
	t.Logf("negative control refused the candy-dir layout: %v", err)
}

// TestCollectSkillBodies_RootLevelCandyUsesItsOwnDir pins the OTHER legal SourceDir shape: a ROOT-LEVEL
// standalone candy (the de-submodule cutover's shape) has its manifest and its repo root in the same
// directory, so nothing is stripped and the repo root IS the candy dir.
func TestCollectSkillBodies_RootLevelCandyUsesItsOwnDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".agents", "skills", "review", "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review a PR\n---\n\nRoot-level body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	layers := map[string]CandyModel{
		"review-standalone": NewSpecCandyModel(
			spec.CandyModel{Name: "review-standalone", SourceDir: dir},
			spec.CandyView{Name: "review-standalone", Remote: true, RepoPath: "github.com/opencharly/review-standalone"},
		),
	}
	cfg := &spec.Config{Skills: map[string]json.RawMessage{
		"review": json.RawMessage(`{"owner":"review-standalone","family":"charly-internals"}`),
	}}
	got, err := CollectSkillBodies(cfg, layers)
	if err != nil {
		t.Fatalf("a root-level candy's own directory is its repo root: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Content, "Root-level body.") {
		t.Fatalf("want the root-level body, got %#v", got)
	}
}

// TestCollectSkillBodies_RemoteSubpathCandyResolvesToRepoRoot is the case the 48 unreachable nodes are
// made of: a REMOTE candy fetched from a multi-candy repo. The loader records the in-repo subpath
// (`candy/`), so the repo root is derived by stripping it — and the body must be found at the ROOT,
// never inside the candy dir.
func TestCollectSkillBodies_RemoteSubpathCandyResolvesToRepoRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills", "arch-test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "skills", "arch-test", "SKILL.md"),
		[]byte("---\nname: arch-test\ndescription: arch test box\n---\n\nTest box.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	layers := map[string]CandyModel{
		"arch-test": NewSpecCandyModel(
			spec.CandyModel{Name: "arch-test", SourceDir: filepath.Join(repo, "candy", "arch-test")},
			spec.CandyView{Name: "arch-test", Remote: true, RepoPath: "github.com/opencharly/distro-arch", SubPathPrefix: "candy/"},
		),
	}
	cfg := &spec.Config{Skills: map[string]json.RawMessage{
		"arch-test-skill": json.RawMessage(`{"name":"arch-test","owner":"arch-test","family":"distros"}`),
	}}
	got, err := CollectSkillBodies(cfg, layers)
	if err != nil {
		t.Fatalf("a remote subpath candy must resolve its body at the repo root: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Content, "Test box.") {
		t.Fatalf("want the repo-root body, got %#v", got)
	}
}

func TestCollectSkills_StillEmptyWithoutABox(t *testing.T) {
	// The composition gate stays in CollectSkills: this is the behaviour CollectSkillBodies exists to
	// compensate for, not to replace.
	cfg, layers := skillBodiesFixture(t, true)
	got, err := CollectSkills(cfg, layers, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("CollectSkills must stay composition-gated: got %d entries", len(got))
	}
}
