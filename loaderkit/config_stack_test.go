package loaderkit

// config_stack_test.go — the LAYERED CONFIG STACK (system → in-dir, later files
// winning). The raw-document merge preserves every node (P1 — a typed UnifiedFile
// parse drops the opaque candy bodies). The per-host DEPLOY OVERLAY is NOT a
// stack layer (it is runtime state, applied by the designed per-field merge) —
// TestConfigStack_DeployOverlayIsNotALayer pins that, the R1 regression.

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
// overridden by the in-dir layer (later wins).
func TestConfigStack_LaterWins(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir()
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: system-alpha\n")
	writeLayer(t, project, spec.UnifiedFileName, "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: project-alpha\nbeta:\n    candy:\n        version: 2026.249.2125\n        description: project-beta\n")
	t.Setenv(SystemConfigEnv, system)

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
		t.Errorf("alpha.description = %v, want project-alpha (in-dir wins over system)", got)
	}
	if _, ok := merged["beta"]; !ok {
		t.Error("beta (project-only) missing from the merged doc")
	}
}

// TestConfigStack_SystemLayerFormsProject — without an in-dir layer, the system
// layer alone forms a project (the packaged /etc/charly fallback).
func TestConfigStack_SystemLayerFormsProject(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir() // no charly.yml in the project dir
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\nalpha:\n    candy:\n        version: 2026.249.2125\n        description: system-alpha\n")
	t.Setenv(SystemConfigEnv, system)

	data, ok, err := readConfigStack(project)
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if !ok {
		t.Fatal("readConfigStack: ok=false, want the system layer to form a project")
	}
	if !strings.Contains(string(data), "system-alpha") {
		t.Errorf("merged doc lacks the system layer's candy: %s", data)
	}
}

// TestConfigStack_VersionLaterWins — the merged version is the LATER layer's.
func TestConfigStack_VersionLaterWins(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir()
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\n")
	writeLayer(t, project, spec.UnifiedFileName, "version: 2026.250.0001\n")
	t.Setenv(SystemConfigEnv, system)

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
	data, ok, err := readConfigStack(t.TempDir())
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if ok || data != nil {
		t.Fatalf("readConfigStack = (%v, %v), want (nil, false)", data != nil, ok)
	}
}

// TestConfigStack_DeployOverlayIsNotALayer is the R1 regression for the peer-name
// collision: the per-host DEPLOY OVERLAY (CHARLY_DEPLOY_CONFIG) must NOT enter the
// project stack. A member-bearing bed declares its holder/taker NESTED under the bed
// root, while the overlay persists each member FLATTENED to a top-level key (so each
// member is independently addressable). Raw-merging the overlay as a document layer put
// both copies into the authored deploy map, and FoldMembers then hard-failed on the
// collision (`peer name "preempt-holder" ... collides with an existing deploy/bed/peer
// entry`) — the live failure on every member-bearing bed. The overlay is applied only
// by the designed per-field merge onto the loaded tree (deploykit.MergeDeployConfigs).
func TestConfigStack_DeployOverlayIsNotALayer(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir()
	// The project: a bed root with a NESTED member.
	writeLayer(t, project, spec.UnifiedFileName,
		"version: 2026.249.2125\nbed:\n    pod:\n        image: img\n        disposable: true\n    holder:\n        pod:\n            image: img\n            preemptible:\n                holds: [test-lock]\n")
	// The overlay: the same member persisted TOP-LEVEL (MarshalDeployNode unwraps the
	// member tree to siblings so a member's own `charly config` can address it by name).
	overlay := writeLayer(t, tmp, "overlay.yml",
		"version: 2026.249.2125\nholder:\n    pod:\n        image: img\n        preemptible:\n            holds: [test-lock]\n")
	t.Setenv(SystemConfigEnv, filepath.Join(tmp, "absent.yml"))
	t.Setenv(spec.DeployConfigEnv, overlay)

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
	if _, leaked := merged["holder"]; leaked {
		t.Fatalf("deploy overlay leaked into the project stack as a top-level 'holder' entry — the FoldMembers collision returns: %s", data)
	}
	bed, _ := merged["bed"].(map[string]any)
	if _, ok := bed["holder"]; !ok {
		t.Fatalf("the project's nested member 'holder' is missing from the merged doc: %s", data)
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
