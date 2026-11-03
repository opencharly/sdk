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
// — the same default the generator applies, and the directory the file lives in. Two callers need
// the same decode for different questions, so it lives here rather than twice:
//
//   - CollectSkills asks "which skills ride THIS IMAGE's label?" — a per-BOX question, answered
//     with the composed-candy-chain filter.
//   - CandyModelOwnedSkills asks "which skills does THIS CANDY own?" — a per-CANDY question with
//     no chain involved, projected onto the wire model so a consumer holding only the model can
//     answer it (opencharly/spec#254).
//
// A body that fails to decode is an error, matching CollectSkills: silently dropping it would
// ship a model (or a label) missing a skill that the authored config declares.
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
// CollectSkills filters by COMPOSITION because a label must stay bounded, and a candy that no image
// composes owns its skills all the same. Asking the box question here would make a candy's `Skills`
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
