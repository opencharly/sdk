package kit

import (
	"context"
	"errors"
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
	existing   string // the substrate's charly.yml bytes ("" = absent)
	written    string // the last written charly.yml bytes
	getFileErr error  // when set, GetFile FAILS with this error instead of serving existing
}

func (e *fakeRemoteExec) Venue() string                                           { return "ssh://fake" }
func (e *fakeRemoteExec) Kind() string                                            { return "ssh" }
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
	if e.getFileErr != nil {
		return nil, e.getFileErr
	}
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
func (e *fakeRemoteExec) RunBuilder(_ context.Context, _ spec.BuilderRunOpts) ([]byte, error) {
	return nil, nil
}
func (e *fakeRemoteExec) RunInteractive(_ context.Context, _ string) (int, error) { return 0, nil }
func (e *fakeRemoteExec) RunStream(_ context.Context, _ string) (int, error)      { return 0, nil }

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

// The substrate ledger is legitimately absent on a fresh guest, so a not-found from GetFile is
// the NORMAL first-deploy case, never a failure. These two error texts are the real ones an
// executor returns: `ssh cat` on a path that does not exist, and a genuine read failure. The
// first must be tolerated; the second must still propagate.
const (
	remoteLedgerMissing = "ssh cat ~/.config/charly/charly.yml: exit status 1 " +
		"(stderr: cat: /home/arch/.config/charly/charly.yml: No such file or directory)"
	remoteLedgerDenied = "ssh cat ~/.config/charly/charly.yml: exit status 1 " +
		"(stderr: cat: /home/arch/.config/charly/charly.yml: Permission denied)"
)

// TestAddCandyDeploymentVia_ToleratesAbsentLedger is the regression guard for the first deploy
// into a fresh VM: GetFile reports the absent ledger as a not-found error, and the candy record
// must still be written (the substrate charly.yml gets created from nothing).
func TestAddCandyDeploymentVia_ToleratesAbsentLedger(t *testing.T) {
	exec := &fakeRemoteExec{getFileErr: errors.New(remoteLedgerMissing)}
	paths := &LedgerPaths{ConfigFile: "/tmp/fake/charly.yml", LockFile: "/tmp/fake/charly.yml.lock"}
	if err := AddCandyDeploymentVia(exec, paths, "socat", "deploy-1", nil); err != nil {
		t.Fatalf("AddCandyDeploymentVia on an absent ledger: %v", err)
	}
	for _, want := range []string{"ledger:", "socat:", "candy: socat", "deploy-1"} {
		if !strings.Contains(exec.written, want) {
			t.Fatalf("no ledger was written for an absent substrate charly.yml (missing %q):\n%s",
				want, exec.written)
		}
	}
}

// TestWriteDeployRecordVia_ToleratesAbsentLedger is the same guard for the deploy-record site.
func TestWriteDeployRecordVia_ToleratesAbsentLedger(t *testing.T) {
	exec := &fakeRemoteExec{getFileErr: errors.New(remoteLedgerMissing)}
	paths := &LedgerPaths{ConfigFile: "/tmp/fake/charly.yml", LockFile: "/tmp/fake/charly.yml.lock"}
	rec := &DeployRecord{DeployID: "deploy-1", Target: "arch.arch-host", DeployedAt: "2026-10-04T04:00:00Z"}
	if err := WriteDeployRecordVia(exec, paths, rec); err != nil {
		t.Fatalf("WriteDeployRecordVia on an absent ledger: %v", err)
	}
	for _, want := range []string{"ledger:", "deploys:", "deploy-1"} {
		if !strings.Contains(exec.written, want) {
			t.Fatalf("no deploy record was written for an absent substrate charly.yml (missing %q):\n%s",
				want, exec.written)
		}
	}
}

// TestVia_ToleratesOnlyNotFound proves the guard was NARROWED, not deleted: a read failure
// that is not a not-found must still return to the caller rather than being mistaken for an
// absent ledger — writing a fresh charly.yml over a file we could not read would destroy it.
func TestVia_ToleratesOnlyNotFound(t *testing.T) {
	paths := &LedgerPaths{ConfigFile: "/tmp/fake/charly.yml", LockFile: "/tmp/fake/charly.yml.lock"}

	t.Run("AddCandyDeploymentVia", func(t *testing.T) {
		exec := &fakeRemoteExec{existing: "ledger:\n    candies: {}\n", getFileErr: errors.New(remoteLedgerDenied)}
		if err := AddCandyDeploymentVia(exec, paths, "socat", "deploy-1", nil); err == nil {
			t.Fatal("AddCandyDeploymentVia swallowed a non-not-found read error")
		}
		if exec.written != "" {
			t.Fatalf("AddCandyDeploymentVia wrote to the substrate despite an unreadable ledger:\n%s", exec.written)
		}
	})

	t.Run("WriteDeployRecordVia", func(t *testing.T) {
		exec := &fakeRemoteExec{existing: "ledger:\n    deploys: {}\n", getFileErr: errors.New(remoteLedgerDenied)}
		rec := &DeployRecord{DeployID: "deploy-1", Target: "arch.arch-host", DeployedAt: "2026-10-04T04:00:00Z"}
		if err := WriteDeployRecordVia(exec, paths, rec); err == nil {
			t.Fatal("WriteDeployRecordVia swallowed a non-not-found read error")
		}
		if exec.written != "" {
			t.Fatalf("WriteDeployRecordVia wrote to the substrate despite an unreadable ledger:\n%s", exec.written)
		}
	})
}
