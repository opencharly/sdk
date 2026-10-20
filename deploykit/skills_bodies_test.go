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
// composition involved. Both controls: the same tree passes with the body beside the candy and fails with
// the body at the repo root. The pre-change code path (CollectSkills) cannot see either, because
// BoxCandyChain is empty for a repo that composes no box — which is the defect in
// opencharly/opencharly#530.

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

func TestCollectSkillBodies_BesideTheCandy(t *testing.T) {
	cfg, layers := skillBodiesFixture(t, false)
	got, err := CollectSkillBodies(cfg, layers)
	if err != nil {
		t.Fatalf("body beside the candy must resolve: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if !strings.Contains(got[0].Content, "Reviews things.") {
		t.Fatalf("content not read back from the file: %q", got[0].Content)
	}
}

func TestCollectSkillBodies_AtTheRepoRootFails(t *testing.T) {
	cfg, layers := skillBodiesFixture(t, true)
	if _, err := CollectSkillBodies(cfg, layers); err == nil {
		t.Fatal("body at the repo root must be an error — the loader reads beside the candy, not at the root")
	}
}

func TestCollectSkills_StillEmptyWithoutABox(t *testing.T) {
	// The composition gate stays in CollectSkills: this is the behaviour CollectSkillBodies exists to
	// compensate for, not to replace.
	cfg, layers := skillBodiesFixture(t, false)
	got, err := CollectSkills(cfg, layers, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("CollectSkills must stay composition-gated: got %d entries", len(got))
	}
}
