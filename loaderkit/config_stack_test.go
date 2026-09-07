package loaderkit

// config_stack_test.go — the LAYERED CONFIG STACK (system → user → in-dir,
// later files winning). The raw-document merge preserves every node (P1 — a
// typed UnifiedFile parse drops the opaque candy bodies).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// writeLayer writes a config layer file and returns its path.
func writeLayer(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestConfigStack_LaterWins — a candy node declared in the system layer is
// overridden by the user layer, overridden by the in-dir layer (later wins).
func TestConfigStack_LaterWins(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir()
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: system-alpha\n")
	user := writeLayer(t, tmp, "user.yml", "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: user-alpha\n")
	writeLayer(t, project, spec.UnifiedFileName, "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: project-alpha\nbeta:\n    candy:\n        version: 2026.249.2125\n        description: project-beta\n")
	t.Setenv(SystemConfigEnv, system)
	t.Setenv(spec.DeployConfigEnv, user)

	data, ok, err := readConfigStack(project)
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if !ok {
		t.Fatal("readConfigStack: ok=false, want a project")
	}
	var merged map[string]any
	if err := yaml.Unmarshal(data, &merged); err != nil {
		t.Fatalf("parse merged: %v", err)
	}
	alpha, ok := merged["alpha"].(map[string]any)["candy"].(map[string]any)
	if !ok {
		t.Fatalf("merged alpha candy missing: %v", merged["alpha"])
	}
	if got := alpha["description"]; got != "project-alpha" {
		t.Errorf("alpha.description = %v, want project-alpha (in-dir wins over user and system)", got)
	}
	if _, ok := merged["beta"]; !ok {
		t.Error("beta (project-only) missing from the merged doc")
	}
}

// TestConfigStack_UserWinsOverSystem — without an in-dir layer, the user layer
// wins over the system layer (and the stack alone forms a project).
func TestConfigStack_UserWinsOverSystem(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir() // no charly.yml in the project dir
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: system-alpha\n")
	user := writeLayer(t, tmp, "user.yml", "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: user-alpha\n")
	t.Setenv(SystemConfigEnv, system)
	t.Setenv(spec.DeployConfigEnv, user)

	data, ok, err := readConfigStack(project)
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if !ok {
		t.Fatal("readConfigStack: ok=false, want the system/user layers to form a project")
	}
	s := string(data)
	if !strings.Contains(s, "user-alpha") {
		t.Errorf("merged doc lacks the user layer's override: %s", s)
	}
	if strings.Contains(s, "system-alpha") {
		t.Errorf("merged doc kept the system layer value over the user layer: %s", s)
	}
}

// TestConfigStack_VersionLaterWins — the merged version is the LATER layer's.
func TestConfigStack_VersionLaterWins(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir()
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\n")
	user := writeLayer(t, tmp, "user.yml", "version: 2026.249.2000\n")
	writeLayer(t, project, spec.UnifiedFileName, "version: 2026.250.0001\n")
	t.Setenv(SystemConfigEnv, system)
	t.Setenv(spec.DeployConfigEnv, user)

	data, _, err := readConfigStack(project)
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if !strings.Contains(string(data), "version: 2026.250.0001") {
		t.Errorf("merged version = %s, want the project layer's (later wins)", data)
	}
}

// TestConfigStack_NoProject — no layer exists → (nil, false).
func TestConfigStack_NoProject(t *testing.T) {
	t.Setenv(SystemConfigEnv, filepath.Join(t.TempDir(), "absent.yml"))
	t.Setenv(spec.DeployConfigEnv, filepath.Join(t.TempDir(), "absent.yml"))
	data, ok, err := readConfigStack(t.TempDir())
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if ok || data != nil {
		t.Fatalf("readConfigStack = (%v, %v), want (nil, false)", data != nil, ok)
	}
}

// TestMergeRaw_PreservesCandyNodes — the raw-document merge preserves the
// opaque candy bodies the typed UnifiedFile parse drops (the P1 proof).
func TestMergeRaw_PreservesCandyNodes(t *testing.T) {
	layers := [][]byte{
		[]byte("version: 2026.249.2125\ncharly-mcp:\n    candy:\n        version: 2026.249.2125\n        description: system\n        bake_plugin:\n            - '@github.com/opencharly/plugin-mcp/candy/plugin-mcp:v1'\n"),
	}
	merged, err := mergeConfigStackRaw(layers)
	if err != nil {
		t.Fatalf("mergeConfigStackRaw: %v", err)
	}
	s := string(merged)
	if !strings.Contains(s, "bake_plugin") || !strings.Contains(s, "plugin-mcp") {
		t.Errorf("merged doc lost the candy node: %s", s)
	}
}

// TestConfigStack_RejectsDuplicateKeysWithinLayer — a SINGLE document whose
// top-level mapping repeats a key (the same entity name declared twice, e.g.
// `redis:` as a candy AND as a local node) must ERROR at the stack merge.
// The raw yaml.Node round-trip PRESERVES the duplicate (the merge idx-map is
// what would silently collapse it last-wins), and collapsing it would make the
// parse-level "duplicate top-level entity name" contract (parse.go) unreachable
// — the charly TestCrossKindNameReuse_LoaderAcceptsAllKinds pin.
func TestConfigStack_RejectsDuplicateKeysWithinLayer(t *testing.T) {
	// The RCA's 3-key dupDoc (version/redis/redis): the duplicate `redis`
	// must surface the duplicate-name contract error, not merge away.
	dupDoc := []byte("version: 2026.250.0731\nredis:\n    candy:\n        base: fedora\nredis:\n    local:\n        candy: [redis]\n")
	_, err := mergeConfigStackRaw([][]byte{dupDoc})
	if err == nil {
		t.Fatal("mergeConfigStackRaw silently collapsed a duplicate top-level key within one layer; want a duplicate-name error")
	}
	if !strings.Contains(err.Error(), "duplicate top-level entity name") {
		t.Errorf("error = %v, want it to carry the parse-level 'duplicate top-level entity name' contract message", err)
	}
}

// TestConfigStack_CrossLayerDuplicateKeyStillMerges — duplicate keys ACROSS
// layers are the config-stack precedence itself: the same top-level key in two
// different layers merges later-wins WITHOUT an error. Only WITHIN-layer
// duplicates are rejected (the parse contract); cross-layer merging is
// unaffected by the new guard.
func TestConfigStack_CrossLayerDuplicateKeyStillMerges(t *testing.T) {
	layers := [][]byte{
		[]byte("version: 2026.250.0731\nredis:\n    candy:\n        base: fedora\n"),
		[]byte("version: 2026.250.0731\nredis:\n    local:\n        candy: [redis]\n"),
	}
	merged, err := mergeConfigStackRaw(layers)
	if err != nil {
		t.Fatalf("cross-layer duplicate key must still merge (later wins): %v", err)
	}
	s := string(merged)
	if !strings.Contains(s, "local:") || strings.Contains(s, "base: fedora") {
		t.Errorf("later layer should win for the shared key; merged doc = %s", s)
	}
}
