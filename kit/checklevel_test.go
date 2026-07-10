package kit

import "testing"

// The ladder ordering + default are load-bearing for the bed-runner depth dispatch — a
// regression here would silently change how deep every bed runs.
func TestCheckLevelReaches(t *testing.T) {
	cases := []struct {
		have, want string
		reaches    bool
	}{
		{CheckLevelNone, CheckLevelBuild, false},
		{CheckLevelBuild, CheckLevelBuild, true},
		{CheckLevelBuild, CheckLevelNoAgent, false},
		{CheckLevelNoAgent, CheckLevelNoAgent, true},
		{CheckLevelNoAgent, CheckLevelAgent, false},
		{CheckLevelAgent, CheckLevelAgent, true},
		{CheckLevelAgent, CheckLevelBuild, true},
		{"", CheckLevelNoAgent, true}, // empty resolves to the noagent default
		{"", CheckLevelAgent, false},  // ...which does NOT reach agent
		{"", CheckLevelBuild, true},   // ...but does reach build
	}
	for _, c := range cases {
		if got := CheckLevelReaches(c.have, c.want); got != c.reaches {
			t.Errorf("CheckLevelReaches(%q, %q) = %v, want %v", c.have, c.want, got, c.reaches)
		}
	}
}

func TestResolveCheckLevel_DefaultsToNoAgent(t *testing.T) {
	if got := ResolveCheckLevel(""); got != CheckLevelNoAgent {
		t.Errorf("ResolveCheckLevel(\"\") = %q, want %q", got, CheckLevelNoAgent)
	}
	if got := ResolveCheckLevel("agent"); got != CheckLevelAgent {
		t.Errorf("ResolveCheckLevel(\"agent\") = %q, want %q", got, CheckLevelAgent)
	}
}

func TestIsValidCheckLevel(t *testing.T) {
	for _, ok := range []string{"none", "build", "noagent", "agent"} {
		if !IsValidCheckLevel(ok) {
			t.Errorf("IsValidCheckLevel(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "full", "deploy", "AGENT"} {
		if IsValidCheckLevel(bad) {
			t.Errorf("IsValidCheckLevel(%q) = true, want false", bad)
		}
	}
}
