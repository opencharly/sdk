package loaderkit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestScanInlineCandy_AgentProvideAndTerminalProfiles proves populateFromYAML carries the
// authored agent_provide:/terminal_profile: candy-manifest keys onto CandyView — the scan-time
// half of the W9 gap surfaced merging origin/main's federated control-plane commit into the
// CandyReader retype (the wrap-time half is TestSpecCandyAdapter_AgentProvideAndTerminalProfiles
// in sdk/deploykit).
func TestScanInlineCandy_AgentProvideAndTerminalProfiles(t *testing.T) {
	ly := &spec.CandyYAML{
		Name:         "agent-candy",
		AgentProvide: []spec.AgentRuntimeCapability{{Provider: "pi"}},
		TerminalProfiles: map[string]spec.TerminalProfile{
			"claude-code": {Name: "claude-code", Entrypoint: []string{"claude"}},
		},
	}

	_, v, _ := ScanInlineCandy("agent-candy", t.TempDir(), ly)

	if len(v.AgentProvide) != 1 || v.AgentProvide[0].Provider != "pi" {
		t.Errorf("CandyView.AgentProvide = %v, want [{Provider: pi}]", v.AgentProvide)
	}
	tp, ok := v.TerminalProfiles["claude-code"]
	if !ok || len(tp.Entrypoint) != 1 || tp.Entrypoint[0] != "claude" {
		t.Errorf("CandyView.TerminalProfiles[\"claude-code\"] = %+v, ok=%v, want {Entrypoint: [claude]}, true", tp, ok)
	}
}
