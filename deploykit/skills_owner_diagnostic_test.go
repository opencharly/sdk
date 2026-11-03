package deploykit

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// skills_owner_diagnostic_test.go — the two halves of the skill-body FAILURE contract for an owner
// that the candy set cannot answer for (opencharly/sdk#434: a `candy:` node carrying `base:`/`from:`
// is an IMAGE, routed to the box set, so `layers[owner]` misses and the file is never looked up).
//
// Both cases are about the FINDING, not the resolution rule: which body location a skill must use is
// a separate, still-open decision, but a repo author has to be able to (a) reproduce the same failure
// twice and (b) read what actually happened. Before this file, one tree named 5 different skills over
// 34 runs, and every one of those messages claimed "no SKILL.md exists under .agents/skills beside the
// owning candy" for a path the collector never touched.

// unresolvedSkillsCfg builds a project whose every skill node has no in-line body and whose owner is
// absent from the candy set — the image-candy shape of sdk#434, with no filesystem behind it.
func unresolvedSkillsCfg(t *testing.T, owners ...string) *spec.Config {
	t.Helper()
	skills := make(map[string]json.RawMessage, len(owners))
	for i, owner := range owners {
		skills[fmt.Sprintf("skill-%02d", i)] = mustSkillJSON(t, spec.Skill{
			Family: "test", Owner: owner,
		})
	}
	return &spec.Config{Skills: skills}
}

// TestCollectSkillBodies_UnresolvableOwnerIsNamedAsSuch pins the MESSAGE: an owner the candy set does
// not hold is reported as exactly that, never as an absent file under a path the collector skipped.
func TestCollectSkillBodies_UnresolvableOwnerIsNamedAsSuch(t *testing.T) {
	cfg := unresolvedSkillsCfg(t, "pimage")
	_, err := CollectSkillBodies(cfg, map[string]CandyModel{})
	if err == nil {
		t.Fatal("a skill with no in-line body and an owner outside the candy set must be reported")
	}
	msg := err.Error()
	if !strings.Contains(msg, `owner "pimage" is not a candy of this project`) {
		t.Fatalf("the finding must name the real cause — the owner is not a candy of this project; got %q", msg)
	}
	if strings.Contains(msg, "no SKILL.md exists under") {
		t.Fatalf("the finding must not claim a file search that never ran (the owner is absent from the candy set, so no path is probed); got %q", msg)
	}
}

// TestCollectSkillBodies_FirstFailureIsDeterministic pins the ORDER: the collector returns on its
// first finding, so a map-order walk reports a different skill on every run of one tree.
func TestCollectSkillBodies_FirstFailureIsDeterministic(t *testing.T) {
	cfg := unresolvedSkillsCfg(t, "img-a", "img-b", "img-c", "img-d", "img-e", "img-f", "img-g", "img-h")
	layers := map[string]CandyModel{}
	const runs = 200
	first := ""
	for i := 0; i < runs; i++ {
		_, err := CollectSkillBodies(cfg, layers)
		if err == nil {
			t.Fatalf("run %d: expected a finding", i)
		}
		if first == "" {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("the reported skill changes between runs of one tree — run 0 said %q, run %d said %q",
				first, i, err.Error())
		}
	}
	if !strings.Contains(first, `"skill-00"`) {
		t.Fatalf("the first finding must be the lowest node name (skill-00); got %q", first)
	}
}
