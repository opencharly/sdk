package kit

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// install_ledger_test.go — the ledger I/O: the `ledger:` section of the per-host
// charly.yml. The local path is exercised by the plugin-fleet/plugin-substrate
// tests; here we cover the executor-routed variants (nested deploys) with a fake
// non-local executor.

// fakeRemoteExec is a non-local DeployExecutor that captures the written file
// content (the substrate's charly.yml) and returns a canned existing file.
type fakeRemoteExec struct {
	existing string // the substrate's charly.yml bytes ("" = absent)
	written  string // the last written charly.yml bytes
}

func (e *fakeRemoteExec) Venue() string { return "ssh://fake" }
func (e *fakeRemoteExec) Kind() string { return "ssh" }
func (e *fakeRemoteExec) RunSystem(_ context.Context, _ string, _ EmitOpts) error { return nil }
func (e *fakeRemoteExec) RunUser(_ context.Context, s string, _ EmitOpts) error {
	// Extract the heredoc body (the charly.yml bytes) between the opening
	// <<'CHARLY_LEDGER_EOF' and the closing CHARLY_LEDGER_EOF.
	const open = "<<'CHARLY_LEDGER_EOF'\n"
	const close = "\nCHARLY_LEDGER_EOF"
	i := strings.Index(s, open)
	j := strings.Index(s, close)
	if i >= 0 && j > i {
		e.written = s[i+len(open) : j]
	}
	return nil
}
func (e *fakeRemoteExec) PutFile(_ context.Context, _ string, _ string, _ uint32, _ bool, _ EmitOpts) error {
	return nil
}
func (e *fakeRemoteExec) GetFile(_ context.Context, _ string, _ bool, _ EmitOpts) ([]byte, error) {
	if e.existing == "" {
		return nil, nil
	}
	return []byte(e.existing), nil
}
func (e *fakeRemoteExec) RunCapture(_ context.Context, _ string) (string, string, int, error) {
	return "/home/u", "", 0, nil
}
func (e *fakeRemoteExec) ResolveHome(_ context.Context, _ string) (string, error) {
	return "/home/u", nil
}
func (e *fakeRemoteExec) RunHostStep(_ context.Context, _ spec.InstallStepView, _ []byte) ([]spec.ReverseOp, error) {
	return nil, nil
}
func (e *fakeRemoteExec) RunBuilder(_ context.Context, _ spec.BuilderRunOpts) ([]byte, error) { return nil, nil }
func (e *fakeRemoteExec) RunInteractive(_ context.Context, _ string) (int, error) { return 0, nil }
func (e *fakeRemoteExec) RunStream(_ context.Context, _ string) (int, error) { return 0, nil }

// TestAddCandyDeploymentVia_WritesLedgerSection proves the executor-routed
// variant writes the candy record into the `ledger:` section of the substrate's
// charly.yml with the Candy + DeployedAt fields populated (regression: the first
// cutover unmarshaled the WHOLE charly.yml into a CandyRecord, producing empty
// candy/deployed_at and failing egress validation).
func TestAddCandyDeploymentVia_WritesLedgerSection(t *testing.T) {
	exec := &fakeRemoteExec{}
	paths := &LedgerPaths{ConfigFile: "/tmp/fake/charly.yml", LockFile: "/tmp/fake/charly.yml.lock"}
	if err := AddCandyDeploymentVia(exec, paths, "socat", "deploy-1", nil); err != nil {
		t.Fatalf("AddCandyDeploymentVia: %v", err)
	}
	if !strings.Contains(exec.written, "ledger:") {
		t.Fatalf("written charly.yml has no ledger: section:\n%s", exec.written)
	}
	if !strings.Contains(exec.written, "socat:") {
		t.Fatalf("written charly.yml has no socat candy record:\n%s", exec.written)
	}
	if !strings.Contains(exec.written, "candy: socat") {
		t.Fatalf("candy record lost its Candy field:\n%s", exec.written)
	}
	if !strings.Contains(exec.written, "deployed_at:") {
		t.Fatalf("candy record lost its DeployedAt field:\n%s", exec.written)
	}
	if !strings.Contains(exec.written, "deploy-1") {
		t.Fatalf("candy record lost its DeployedBy entry:\n%s", exec.written)
	}
}

// TestAddCandyDeploymentVia_PreservesExistingKeys proves the executor-routed
// variant preserves the substrate's existing charly.yml keys (deploy:, cache:)
// when updating the ledger section.
func TestAddCandyDeploymentVia_PreservesExistingKeys(t *testing.T) {
	exec := &fakeRemoteExec{existing: "version: 2026.240.1943\ncache:\n    git:\n        latest_tags: {}\nweb-local:\n    pod:\n        image: web\n"}
	paths := &LedgerPaths{ConfigFile: "/tmp/fake/charly.yml", LockFile: "/tmp/fake/charly.yml.lock"}
	if err := AddCandyDeploymentVia(exec, paths, "socat", "deploy-1", nil); err != nil {
		t.Fatalf("AddCandyDeploymentVia: %v", err)
	}
	for _, want := range []string{"version: 2026.240.1943", "cache:", "web-local:", "image: web", "ledger:", "socat:"} {
		if !strings.Contains(exec.written, want) {
			t.Fatalf("written charly.yml lost %q:\n%s", want, exec.written)
		}
	}
}

// TestLedgerRelocatedFailsLoudly proves a consumer still using the LEGACY
// LedgerPaths fields (Root/Deploys/Candies — the deleted JSON-file layout) gets
// an explicit ErrLedgerRelocated instead of a silent wrong answer (e.g.
// os.Stat("") → ENOENT → "no local deploys").
func TestLedgerRelocatedFailsLoudly(t *testing.T) {
	legacy := &LedgerPaths{Root: "/tmp/legacy", Deploys: "/tmp/legacy/deploys", Candies: "/tmp/legacy/layers"}
	if err := WriteDeployRecord(legacy, &DeployRecord{DeployID: "x"}); err != ErrLedgerRelocated {
		t.Fatalf("WriteDeployRecord: got %v, want ErrLedgerRelocated", err)
	}
	if _, err := ReadDeployRecord(legacy, "x"); err != ErrLedgerRelocated {
		t.Fatalf("ReadDeployRecord: got %v, want ErrLedgerRelocated", err)
	}
	if err := WriteCandyRecord(legacy, &CandyRecord{Candy: "x"}); err != ErrLedgerRelocated {
		t.Fatalf("WriteCandyRecord: got %v, want ErrLedgerRelocated", err)
	}
	if _, err := ReadCandyRecord(legacy, "x"); err != ErrLedgerRelocated {
		t.Fatalf("ReadCandyRecord: got %v, want ErrLedgerRelocated", err)
	}
	if err := DeleteDeployRecord(legacy, "x"); err != ErrLedgerRelocated {
		t.Fatalf("DeleteDeployRecord: got %v, want ErrLedgerRelocated", err)
	}
	if err := DeleteCandyRecord(legacy, "x"); err != ErrLedgerRelocated {
		t.Fatalf("DeleteCandyRecord: got %v, want ErrLedgerRelocated", err)
	}
	if err := AddCandyDeployment(legacy, "x", "d", nil); err != ErrLedgerRelocated {
		t.Fatalf("AddCandyDeployment: got %v, want ErrLedgerRelocated", err)
	}
	if _, _, err := RemoveCandyDeployment(legacy, "x", "d"); err != ErrLedgerRelocated {
		t.Fatalf("RemoveCandyDeployment: got %v, want ErrLedgerRelocated", err)
	}
}
