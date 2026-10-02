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
// layer's OWN candy node forms a project (the packaged fallback that survives the
// retired-directive strip; a stamp-only layer contributes nothing — see
// TestConfigStack_RetiredOnlySystemLayerContributesNothing).
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

// TestConfigStack_RetiredSystemVersionIsDropped — the SYSTEM layer's `version:` stamp
// must NOT reach the merged document. The system file is package-RENDERED, never
// authored, and spec #183 removed the top-level `version` from the closed #NodeDoc, so
// merging the stamp made EVERY project on a host carrying it unresolvable
// (`conflicting values … mismatched types string and struct`) — an empty HOME/XDG test
// could not see it, because the stack layer is not user config.
func TestConfigStack_RetiredSystemVersionIsDropped(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir()
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\n")
	writeLayer(t, project, spec.UnifiedFileName,
		"alpha:\n    candy:\n        version: 2026.249.2125\n        description: project-alpha\n")
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
	if v, leaked := merged["version"]; leaked {
		t.Fatalf("the retired system-layer `version:` stamp leaked into the merged document (%v) — on a host carrying it the closed #NodeDoc rejects every project", v)
	}
	// Only the TOP-LEVEL key is retired: a candy node's own nested version is a
	// different field and must survive the strip untouched.
	alpha, _ := merged["alpha"].(map[string]any)
	candy, _ := alpha["candy"].(map[string]any)
	if got := candy["version"]; got != "2026.249.2125" {
		t.Errorf("alpha.candy.version = %v, want 2026.249.2125 — the strip must drop only the top-level key: %s", got, data)
	}
}

// TestConfigStack_RetiredOnlySystemLayerContributesNothing — the document a package
// actually renders is (just) the retired stamp. Stripped, it leaves nothing, so the
// layer is SKIPPED rather than merged as an empty mapping: a host carrying only the
// stamp resolves exactly like a host with no system config at all — a project beside it
// is that project alone, and with no project there is no project.
func TestConfigStack_RetiredOnlySystemLayerContributesNothing(t *testing.T) {
	tmp := t.TempDir()
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\n")
	t.Setenv(SystemConfigEnv, system)

	project := t.TempDir()
	writeLayer(t, project, spec.UnifiedFileName, "alpha:\n    description: authored\n")
	data, ok, err := readConfigStack(project)
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if !ok {
		t.Fatal("readConfigStack: ok=false, want the project to form one")
	}
	var merged map[string]any
	if err := yaml.Unmarshal(data, &merged); err != nil {
		t.Fatalf("parse merged: %v", err)
	}
	if len(merged) != 1 {
		t.Errorf("a retired-only system layer changed the merged document: %v", merged)
	}

	data, ok, err = readConfigStack(t.TempDir()) // no charly.yml in the dir
	if err != nil {
		t.Fatalf("readConfigStack (no project): %v", err)
	}
	if ok || data != nil {
		t.Fatalf("a retired-only system layer manufactured a project: (%v, %v), want (nil, false)", data != nil, ok)
	}
}

// TestConfigStack_AuthoredProjectVersionSurvives — the strip is scoped to the SYSTEM
// layer. An AUTHORED project that still carries `version:` keeps it in the merge, so the
// leftover is still a hard error at the schema gate and `charly migrate` still sees the
// key it exists to strip. Dropping it here would silently swallow a real leftover.
func TestConfigStack_AuthoredProjectVersionSurvives(t *testing.T) {
	tmp := t.TempDir()
	project := t.TempDir()
	system := writeLayer(t, tmp, "system.yml", "version: 2026.249.2125\n")
	writeLayer(t, project, spec.UnifiedFileName, "version: 2026.261.1747\n")
	t.Setenv(SystemConfigEnv, system)

	data, _, err := readConfigStack(project)
	if err != nil {
		t.Fatalf("readConfigStack: %v", err)
	}
	if !strings.Contains(string(data), "version: 2026.261.1747") {
		t.Errorf("the authored project's `version:` was dropped from the merge (%s) — the strip must stay scoped to the package-rendered system layer", data)
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
