package deploykit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// skills_collect_test.go — sdk-level coverage for CollectSkills, the ai.opencharly.skill label
// aggregator. Proves the UNION-OF-COMPOSED-ONLY semantics: only skills whose `owner` is a candy
// IN the composed chain (BoxCandyChain) ride the label — concept-candy skills (charly-core,
// charly-internals, …) never do — and the projection maps the authoring spec.Skill onto the
// wire spec.LabelSkillEntry (family/name/content required, refs/triggers/category carried).

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
			// owned by composed candies → must ride the label
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

	got := CollectSkills(cfg, layers, "app")
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

func mustSkillJSON(t *testing.T, s spec.Skill) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal skill: %v", err)
	}
	return b
}
