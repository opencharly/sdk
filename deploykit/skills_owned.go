package deploykit

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/opencharly/spec/spec"
)

// decodeSkillNodes decodes every authored `skill:` node ONCE and returns them keyed by node name.
//
// The map KEY is the node name, which IS the skill id when the node declares no `name:` (spec#247)
// — the same default the generator applies, and the directory the file lives in. Its caller is
// CandyModelOwnedSkills, which asks "which skills does THIS CANDY own?" — a per-CANDY question with
// no chain involved, projected onto the wire model so a consumer holding only the model can answer
// it (opencharly/spec#254). Which skills an IMAGE carries is the per-BOX question, and it is
// kind:skill's own (ops.OpContent, opencharly/plugin-harness-kind), never answered here.
//
// A body that fails to decode is an error: silently dropping it would ship a model missing a skill
// that the authored config declares.
func decodeSkillNodes(skills map[string]json.RawMessage) (map[string]spec.Skill, error) {
	out := make(map[string]spec.Skill, len(skills))
	for node, raw := range skills {
		if len(raw) == 0 {
			continue
		}
		var s spec.Skill
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("skill %q: decode: %w", node, err)
		}
		out[node] = s
	}
	return out, nil
}

// CandyModelOwnedSkills returns the NAMES of the `skill:` nodes this candy owns, sorted, for
// projection onto spec.CandyModel.Skills.
//
// The association is `skill.owner` — "the candy/concept-candy entity name that owns this skill,
// the build-time association that decides which images carry it" (spec/schema/skill.cue) — so the
// question is per-CANDY and is answered WITHOUT a composed-candy chain. That distinction matters:
// kind:skill's content filters by COMPOSITION (an image carries only what it composes), and a candy
// that no image composes owns its skills all the same. Asking the box question here would make a candy's `Skills`
// depend on which box happened to be under it.
//
// Returns nil when the candy owns nothing (and when the nodes are absent), so an empty projection
// is absent rather than an empty list.
func CandyModelOwnedSkills(skills map[string]json.RawMessage, candy string) ([]string, error) {
	if candy == "" || len(skills) == 0 {
		return nil, nil
	}
	nodes, err := decodeSkillNodes(skills)
	if err != nil {
		return nil, err
	}
	var owned []string
	for node, s := range nodes {
		if s.Owner == candy {
			owned = append(owned, node)
		}
	}
	sort.Strings(owned)
	return owned, nil
}

// UnionOwnedSkills merges the skill names a candy owns as projected by ANOTHER scope into the names
// already recorded for it, returning the sorted, de-duplicated union (nil when both are empty).
//
// The resolved-project envelope keys candies by bare name, and one candy is materialized in several
// scopes: the root's remote copy, and each import namespace that reaches it. Only the scope that
// AUTHORS a candy's skill nodes can see its ownership, and it is not necessarily the first scope to
// record the candy — measured on pod-kde-cstream, where `arch-skills` was recorded by a scope
// holding none of its 8 skill nodes and the `arch` namespace that owns 6 of them was then skipped as
// a duplicate (opencharly/sdk#598). Ownership is therefore gathered from every scope, never taken
// from whichever scope happened to arrive first.
func UnionOwnedSkills(recorded, projected []string) []string {
	if len(projected) == 0 {
		return recorded
	}
	seen := make(map[string]bool, len(recorded)+len(projected))
	var out []string
	for _, list := range [][]string{recorded, projected} {
		for _, n := range list {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}
