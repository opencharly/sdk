package agentkit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// ValidateTeam enforces graph invariants that CUE cannot express: unique member
// identities and edges/coordinator that reference declared members. Cycles are
// permitted because peer review loops are valid; authority still comes only
// from explicitly authored edges and allow lists.
func ValidateTeam(team spec.AgentTeam) error {
	members := make(map[string]bool, len(team.Agents))
	for _, member := range team.Agents {
		if members[member.Name] {
			return fmt.Errorf("agent team: duplicate member %q", member.Name)
		}
		members[member.Name] = true
	}
	if team.Coordinator != "" && !members[team.Coordinator] {
		return fmt.Errorf("agent team: coordinator %q is not a member", team.Coordinator)
	}
	seen := map[string]bool{}
	for _, edge := range team.Edges {
		if !members[edge.From] || !members[edge.To] {
			return fmt.Errorf("agent team: edge %q -> %q references an unknown member", edge.From, edge.To)
		}
		key := edge.From + "\x00" + edge.To
		if seen[key] {
			return fmt.Errorf("agent team: duplicate edge %q -> %q", edge.From, edge.To)
		}
		seen[key] = true
	}
	return nil
}

func DelegationAllowed(team spec.AgentTeam, from, to, operation string) bool {
	for _, edge := range team.Edges {
		if edge.From == from && edge.To == to {
			if len(edge.Allow) == 0 {
				return true
			}
			for _, allowed := range edge.Allow {
				if allowed == operation || allowed == "*" {
					return true
				}
			}
		}
	}
	return false
}
