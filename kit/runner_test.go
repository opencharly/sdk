package kit

import (
	"reflect"
	"testing"
)

// TestEffectiveEnv_HostVarsOverlay / TestEffectiveEnv_NoHostVarsReturnsBase: relocated
// from charly/check_members_test.go (#55 decoupling cone, Batch D) — assert
// kit.Runner.EffectiveEnv()'s own behavior directly, zero charly coupling.

// TestEffectiveEnv_HostVarsOverlay: ${HOST:…} addresses overlay onto the active env in
// kit.Runner.EffectiveEnv — the single injection point that makes cross-member
// addressing work for the primary AND on:-swapped venues.
func TestEffectiveEnv_HostVarsOverlay(t *testing.T) {
	base := map[string]string{"USER": "user"}
	kr := NewRunner(RunnerConfig{
		Env:      base,
		HostVars: map[string]string{"HOST:web": "charly-web"},
	})
	env := kr.EffectiveEnv()
	if env["USER"] != "user" {
		t.Errorf("base var lost: %v", env)
	}
	if env["HOST:web"] != "charly-web" {
		t.Errorf("host var not overlaid: %v", env)
	}
	// The base env map must stay clean (copy-on-overlay).
	if _, leaked := base["HOST:web"]; leaked {
		t.Errorf("EffectiveEnv mutated the shared base Env")
	}
}

// TestEffectiveEnv_NoHostVarsReturnsBase: with no HostVars and no Scenario,
// EffectiveEnv returns the base map directly (behaviour unchanged).
func TestEffectiveEnv_NoHostVarsReturnsBase(t *testing.T) {
	base := map[string]string{"USER": "user"}
	kr := NewRunner(RunnerConfig{Env: base})
	if got := kr.EffectiveEnv(); !reflect.DeepEqual(got, base) {
		t.Errorf("EffectiveEnv = %v, want the base map %v", got, base)
	}
}
