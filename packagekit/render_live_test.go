package packagekit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestRenderConfig_ValidatesWithCharly — the rendered /etc/charly/charly.yml
// must PASS `charly box validate` with the released charly (when one is on
// PATH in the test env). Guards the ADE plan gate (a run-only plan fails).
func TestRenderConfig_ValidatesWithCharly(t *testing.T) {
	// Prefer an explicitly-set released charly (CHARLY_TEST_BIN), then a
	// released FHS install, then PATH. A dev/worktree charly rejects the
	// current schema stamp ("newer than this charly supports") — the released
	// binary accepts it.
	var err error
	charly := os.Getenv("CHARLY_TEST_BIN")
	if charly == "" {
		charly, err = exec.LookPath("charly")
		if err != nil {
			t.Skip("no charly on PATH; set CHARLY_TEST_BIN to a released charly")
		}
	}
	cfg := &spec.PackagingConfig{
		Path:        "/etc/charly/charly.yml",
		Version:     "2026.249.2125",
		Description: "System-wide charly MCP server project (started via systemd)",
		Plugins:     []string{"@github.com/opencharly/plugin-mcp/candy/plugin-mcp:v2026.250.0635"},
	}
	body, err := renderConfig(cfg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "charly.yml")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(charly, "box", "validate", "-C", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("charly box validate FAILED on the rendered config:\n%s", out)
	}
}
