package deploykit

// The gates for the plugin half of the shell hook (opencharly/charly#916). The delivery in
// spec/exec/charly_plugins.go puts a plugin tree on the venue and a plan cannot know WHERE by
// digest (it is compiled host-side, before any delivery), so the plan points every venue shell at
// StablePluginsPath and the delivery publishes that name. These tests pin the three cases that
// matter, including the one the old guard vetoed silently.

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestShellHookCarriesPluginsWithoutAnyEnv is the case the previous guard REFUSED: a layer that
// serves plugins and declares no env at all returned nil, so no hook was emitted, so no shell in
// the venue ever learned where the delivered tree was — and a plugin verb in a plan step failed
// there as an unknown command, exit=2, with the loader printing "plugin env: unset".
func TestShellHookCarriesPluginsWithoutAnyEnv(t *testing.T) {
	adapter := NewSpecCandyModel(spec.CandyModel{Name: "plugin-deploy-firecracker"}, spec.CandyView{
		Name:            "plugin-deploy-firecracker",
		PluginProviders: []string{"deploy:firecracker"},
	})

	hook := CompileShellHookStep(adapter, nil)
	if hook == nil {
		t.Fatal("a plugin-serving layer with no env must still produce a shell hook, or the venue never learns where the plugin tree is")
	}
	if got := hook.EnvVars[PluginsEnvVar]; got != StablePluginsPath {
		t.Fatalf("hook.EnvVars[%s] = %q, want %q", PluginsEnvVar, got, StablePluginsPath)
	}
}

// TestShellHookOmitsTheVariableWhenNoPluginsAreServed is the other half: the variable names a
// capability. Emitting it for a layer that delivers no plugin tree would point the loader at a path
// nothing filled — claiming exactly what the deploy did not deliver.
func TestShellHookOmitsTheVariableWhenNoPluginsAreServed(t *testing.T) {
	// Env lives on the CandyModel (adapter.EnvConfig returns a.m.Env), not the view.
	adapter := NewSpecCandyModel(spec.CandyModel{
		Name: "ripgrep",
		Env:  &spec.EnvConfig{Vars: map[string]string{"RIPGREP_CONFIG_PATH": "{{.Home}}/.ripgreprc"}},
	}, spec.CandyView{Name: "ripgrep"})

	hook := CompileShellHookStep(adapter, nil)
	if hook == nil {
		t.Fatal("a layer with env must still produce a hook")
	}
	if _, ok := hook.EnvVars[PluginsEnvVar]; ok {
		t.Fatalf("a layer serving no plugins must not carry %s — it would claim an undelivered capability", PluginsEnvVar)
	}
	if got := hook.EnvVars["RIPGREP_CONFIG_PATH"]; got == "" {
		t.Fatal("the candy's own env must survive the plugin addition")
	}
}

// TestShellHookStillReturnsNilForAnEmptyLayer keeps the original contract: a layer with neither env
// nor plugins contributes no hook, so the plan does not grow a step that says nothing.
func TestShellHookStillReturnsNilForAnEmptyLayer(t *testing.T) {
	adapter := NewSpecCandyModel(spec.CandyModel{Name: "plain"}, spec.CandyView{Name: "plain"})
	if hook := CompileShellHookStep(adapter, nil); hook != nil {
		t.Fatalf("an empty layer must yield no hook, got %+v", hook)
	}
}
