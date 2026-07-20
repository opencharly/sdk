package agentkit

import (
	"github.com/opencharly/sdk/spec"
	"testing"
)

func TestValidateTeamAndDelegationAuthority(t *testing.T) {
	team := spec.AgentTeam{Agents: []spec.AgentTeamMember{{Name: "lead", Runtime: "pi"}, {Name: "review", Runtime: "pi"}}, Coordinator: "lead", Edges: []spec.AgentDelegationEdge{{From: "lead", To: "review", Allow: []string{"review"}}}}
	if err := ValidateTeam(team); err != nil {
		t.Fatal(err)
	}
	if !DelegationAllowed(team, "lead", "review", "review") {
		t.Fatal("authored delegation rejected")
	}
	if DelegationAllowed(team, "review", "lead", "review") {
		t.Fatal("implicit reverse authority invented")
	}
	team.Edges = append(team.Edges, spec.AgentDelegationEdge{From: "lead", To: "ghost"})
	if err := ValidateTeam(team); err == nil {
		t.Fatal("unknown edge member accepted")
	}
}
