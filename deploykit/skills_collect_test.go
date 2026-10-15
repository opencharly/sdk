package deploykit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/candywalk"
	"github.com/opencharly/spec/spec"
)

// skills_collect_test.go — sdk-level coverage for CollectSkills, the ai.opencharly.skill label
// aggregator. Two halves:
//
//   - the UNION-OF-COMPOSED-ONLY semantics: only skills whose `owner` is a candy IN the composed
//     chain (BoxCandyChain) ride the label — concept-candy skills (charly-core, charly-internals,
//     …) never do;
//   - the PROSE READ-BACK: a skill that has moved its body out of the node into the file its repo
//     owns (`<source dir>/.agents/skills/<name>/SKILL.md`) still rides the label whole. Each of
//     these cases fails on the pre-change tree, where the projection copied the node alone and a
//     moved skill shipped an entry with `description` absent and `content` empty.

func TestCollectSkills_UnionOfComposedOnly(t *testing.T) {
	layers := map[string]spec.CandyReader{
		"postgresql": NewSpecCandyModel(
			spec.CandyModel{Name: "postgresql"},
			spec.CandyView{Name: "postgresql", Description: "Postgres"},
		),
		"redis": NewSpecCandyModel(
			spec.CandyModel{Name: "redis"},
			spec.CandyView{Name: "redis", Description: "Redis"},
		),
	}
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"app": spec.EncodeBox(spec.BoxConfig{Candy: []string{"postgresql", "redis"}}),
		},
		Skills: map[string]json.RawMessage{
			// owned by composed candies → must ride the label. No source dir on either candy, so
			// this is also the TRANSITIONAL IN-LINE FORM's case: the node's own body is projected.
			"postgresql-skill": mustSkillJSON(t, spec.Skill{
				Name: "postgresql", Family: "charly-infrastructure", Owner: "postgresql",
				Description: "Use when working with postgresql.",
				Content:     "# Postgresql\nbody",
				References:  []spec.SkillReference{{Name: "config", Content: "config body"}},
				Triggers:    []string{"postgres / postgresql"},
			}),
			"redis-skill": mustSkillJSON(t, spec.Skill{
				Name: "redis", Family: "charly-infrastructure", Owner: "redis",
				Description: "Use when working with redis.",
				Content:     "# Redis\nbody",
			}),
			// owned by a concept candy NOT composed → must NOT ride the label
			"core-skill": mustSkillJSON(t, spec.Skill{
				Name: "charly-status", Family: "charly-core", Owner: "charly-core",
				Description: "charly status.",
				Content:     "# charly status\nbody",
			}),
		},
	}

	got, err := CollectSkills(cfg, layers, "app")
	if err != nil {
		t.Fatalf("CollectSkills: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("CollectSkills = %d entries, want 2 (composed-only union); got %+v", len(got), got)
	}
	byName := map[string]spec.LabelSkillEntry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	pg, ok := byName["postgresql"]
	if !ok {
		t.Fatalf("missing postgresql skill; got %+v", byName)
	}
	if pg.Family != "charly-infrastructure" || pg.Owner != "postgresql" || pg.Content != "# Postgresql\nbody" {
		t.Fatalf("postgresql entry not projected faithfully: %+v", pg)
	}
	if len(pg.References) != 1 || pg.References[0].Name != "config" {
		t.Fatalf("postgresql references not carried: %+v", pg.References)
	}
	if _, bad := byName["charly-status"]; bad {
		t.Fatalf("concept-candy skill must NOT ride the label (union-of-composed-only)")
	}
	// deterministic ordering: sorted by family/name
	if got[0].Name != "postgresql" || got[1].Name != "redis" {
		t.Fatalf("entries not deterministically ordered: %q", got)
	}
}

// TestCollectSkills_ProseComesFromTheFile is the read-back's discriminating case: the node
// carries routing metadata ONLY (no description, no content, no references) and the prose lives
// in the file its repo owns. Before this change the projected entry was `{family, name, owner,
// content:""}` — description absent, content empty — which is the whole label contract degrading
// silently.
func TestCollectSkills_ProseComesFromTheFile(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, root, "postgresql",
		"name: postgresql\ndescription: Use when working with postgresql.\n",
		"\n# Postgresql\nbody from the file\n",
		map[string]string{"config": "config body from the file\n"})

	layers := map[string]spec.CandyReader{
		"postgresql": NewSpecCandyModel(
			spec.CandyModel{Name: "postgresql", SourceDir: root},
			spec.CandyView{Name: "postgresql"},
		),
	}
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"app": spec.EncodeBox(spec.BoxConfig{Candy: []string{"postgresql"}}),
		},
		Skills: map[string]json.RawMessage{
			// The node declares no `name:`, so the NODE NAME is the skill id (spec#247) — the
			// directory the file must live in.
			"postgresql": mustSkillJSON(t, spec.Skill{
				Family: "charly-infrastructure", Owner: "postgresql",
				Triggers: []string{"postgres / postgresql"},
			}),
		},
	}

	got, err := CollectSkills(cfg, layers, "app")
	if err != nil {
		t.Fatalf("CollectSkills: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("CollectSkills = %d entries, want 1; got %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "postgresql" {
		t.Fatalf("entry name = %q, want %q — the node name IS the id when the node declares none", e.Name, "postgresql")
	}
	if e.Description != "Use when working with postgresql." {
		t.Fatalf("entry description = %q, want the file's frontmatter description", e.Description)
	}
	if !strings.Contains(e.Content, "# Postgresql\nbody from the file") {
		t.Fatalf("entry content = %q, want the file's body", e.Content)
	}
	if len(e.References) != 1 || e.References[0].Name != "config" || !strings.Contains(e.References[0].Content, "config body from the file") {
		t.Fatalf("entry references = %+v, want the file's references/config.md", e.References)
	}
	if len(e.Triggers) != 1 {
		t.Fatalf("routing metadata (triggers) must still come from the node: %+v", e)
	}
}

// TestCollectSkills_BodylessSkillFailsLoud pins the LOUD half: a node with neither an in-line body
// nor a file used to project an entry whose prose was simply missing. It is an error now, because
// such an entry is indistinguishable from a skill that never had a body.
func TestCollectSkills_BodylessSkillFailsLoud(t *testing.T) {
	layers := map[string]spec.CandyReader{
		"postgresql": NewSpecCandyModel(
			spec.CandyModel{Name: "postgresql", SourceDir: t.TempDir()},
			spec.CandyView{Name: "postgresql"},
		),
	}
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"app": spec.EncodeBox(spec.BoxConfig{Candy: []string{"postgresql"}}),
		},
		Skills: map[string]json.RawMessage{
			"postgresql": mustSkillJSON(t, spec.Skill{
				Family: "charly-infrastructure", Owner: "postgresql",
			}),
		},
	}

	got, err := CollectSkills(cfg, layers, "app")
	if err == nil {
		t.Fatalf("CollectSkills accepted a body-less skill and produced %+v; want an error naming the missing body", got)
	}
	if !strings.Contains(err.Error(), "no body") {
		t.Fatalf("error = %v, want it to name the missing body", err)
	}
}

// TestCollectSkills_MalformedFileFailsLoud proves a file that would publish the WRONG prose under
// the right name is refused rather than silently projected: the frontmatter `name` must agree with
// the directory, which IS the skill id.
func TestCollectSkills_MalformedFileFailsLoud(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, root, "postgresql",
		"name: not-postgresql\ndescription: something else entirely.\n",
		"\n# Wrong skill\n", nil)

	layers := map[string]spec.CandyReader{
		"postgresql": NewSpecCandyModel(
			spec.CandyModel{Name: "postgresql", SourceDir: root},
			spec.CandyView{Name: "postgresql"},
		),
	}
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"app": spec.EncodeBox(spec.BoxConfig{Candy: []string{"postgresql"}}),
		},
		Skills: map[string]json.RawMessage{
			"postgresql": mustSkillJSON(t, spec.Skill{
				Family: "charly-infrastructure", Owner: "postgresql",
			}),
		},
	}

	if _, err := CollectSkills(cfg, layers, "app"); err == nil {
		t.Fatalf("CollectSkills accepted a file whose frontmatter name disagrees with its directory")
	}
}

// writeSkillFile lays down the authored shape the relocation produces — the two-key frontmatter,
// the body, and references/<stem>.md — under the Agent-Skills standard location.
func writeSkillFile(t *testing.T, root, name, frontmatter, body string, refs map[string]string) {
	t.Helper()
	dir := filepath.Join(root, candywalk.AgentsSkillsDir, name)
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	entry := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(entry), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	for stem, content := range refs {
		if err := os.WriteFile(filepath.Join(dir, "references", stem+".md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write references/%s.md: %v", stem, err)
		}
	}
}

func mustSkillJSON(t *testing.T, s spec.Skill) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal skill: %v", err)
	}
	return b
}
