package kit_test

// secret_delivery_realize_test.go — the REALIZE half of P07 unit 2b, and the only test that drives
// `kit.walkFile` from a compiled step.
//
// The compiler test in sdk/deploykit proves the STEP is emitted with the right Source/Dest/Mode,
// and `TestBuildDeployPlan_CarriesTheSecretDeliveryStep` proves it reaches `InstallPlan.Steps`
// before the candy's own op steps. Neither proves a file lands: `walkFile` has never been driven
// by a compiled plan in this tree (this unit is the first producer of a `spec.FileStep`; the only
// other construction was the wire decoder). So this test goes the whole way —
// compile → view → `kit.WalkPlans` → `DeployExecutor.PutFile` — against a recording executor.
//
// Two assertions, and the SECOND is the one D-SEC-1 is about:
//  1. the bytes arrive at Dest, with mode 0600 (a secret delivered world-readable is not delivered);
//  2. the bytes appear NOWHERE ELSE — not in the plan, not in the serialized views, not in any
//     other step. The delivery is "the value reaches its consumer as a file, never as text", and a
//     test that only checks (1) would pass on a plan that also carried the value in an env var.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// recFile is one recorded PutFile.
type recFile struct {
	content []byte
	mode    uint32
	root    bool
}

// recordingExecutor is a DeployExecutor that records every PutFile and answers the venue probes.
type recordingExecutor struct {
	puts    map[string]recFile
	sysRuns []string
}

func newRecordingExecutor() *recordingExecutor {
	return &recordingExecutor{puts: map[string]recFile{}}
}

func (e *recordingExecutor) Venue(context.Context) (string, error) { return "record://venue", nil }
func (e *recordingExecutor) RunSystem(_ context.Context, s string, _ []byte) error {
	e.sysRuns = append(e.sysRuns, s)
	return nil
}
func (e *recordingExecutor) RunUser(context.Context, string, []byte) error { return nil }
func (e *recordingExecutor) PutFile(_ context.Context, remotePath string, content []byte, mode uint32, ownerRoot bool) error {
	e.puts[remotePath] = recFile{content: content, mode: mode, root: ownerRoot}
	return nil
}
func (e *recordingExecutor) GetFile(context.Context, string, bool) ([]byte, error) {
	return nil, nil
}
func (e *recordingExecutor) RunCapture(context.Context, string) (string, string, int, error) {
	return "/home/venue-user", "", 0, nil
}
func (e *recordingExecutor) RunHostStep(_ context.Context, step spec.InstallStepView, _ []byte) ([]spec.ReverseOp, error) {
	return nil, nil
}

// secretCandy compiles nothing: it returns the CandyModel a scanner would install for an authored
// `use: secret:` block.
func secretCandy(t *testing.T, useYAML string) deploykit.CandyModel {
	t.Helper()
	var m spec.CandyModel
	if err := yaml.Unmarshal([]byte(useYAML), &m); err != nil {
		t.Fatalf("decoding the authored use: block: %v", err)
	}
	m.Name = "lab-signing"
	return deploykit.NewSpecCandyModel(m, spec.CandyView{Name: "lab-signing"})
}

// TestACompiledSecretStepLandsTheFileAndOnlyThere is the realize proof.
func TestACompiledSecretStepLandsTheFileAndOnlyThere(t *testing.T) {
	const secretBytes = "PRIVATE-KEY-MATERIAL-0123456789"
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "id_ed25519")
	if err := os.WriteFile(src, []byte(secretBytes), 0o600); err != nil {
		t.Fatalf("writing the host secret: %v", err)
	}
	const dst = "/run/lab/id_ed25519"

	steps, err := deploykit.CompileSecretSteps(secretCandy(t, `
use:
  secret:
    lab-signing-key:
      src: `+src+`
      dst: `+dst+`
`))
	if err != nil {
		t.Fatalf("CompileSecretSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("compiled %d steps, want 1", len(steps))
	}

	// (2) D-SEC-1 BEFORE the walk: the value is nowhere in the IR the walk receives, nor in the
	// serialized plan a ledger or a log would record.
	views := spec.StepsToView(steps)
	rawJSON, err := json.Marshal(spec.InstallPlanView{Steps: views})
	if err != nil {
		t.Fatalf("marshalling the plan view: %v", err)
	}
	if strings.Contains(string(rawJSON), secretBytes) {
		t.Fatalf("the secret VALUE is in the serialized plan — D-SEC-1: it never enters the IR, a label or a log:\n%s", rawJSON)
	}
	if !strings.Contains(string(rawJSON), dst) {
		t.Fatalf("the plan does not even name the destination %q — the delivery has no target:\n%s", dst, rawJSON)
	}

	// (1) Drive the walk the deploy targets drive.
	exec := newRecordingExecutor()
	if _, err := kit.WalkPlans(context.Background(), exec, []spec.InstallPlanView{{Steps: views}}, kit.WalkOpts{}); err != nil {
		t.Fatalf("WalkPlans: %v", err)
	}
	got, ok := exec.puts[dst]
	if !ok {
		t.Fatalf("nothing landed at %q; PutFile saw %v", dst, keysOf(exec.puts))
	}
	if string(got.content) != secretBytes {
		t.Fatalf("the destination holds %q, want the host file's bytes", got.content)
	}
	if got.mode != 0o600 {
		t.Fatalf("the destination was written with mode %#o, want 0600 — a secret delivered world-readable is not delivered", got.mode)
	}
	if len(exec.puts) != 1 {
		t.Fatalf("the walk wrote %d files, want exactly the one destination: %v", len(exec.puts), keysOf(exec.puts))
	}
}

// TestTheSecretStepRefusesAnUnreadableSource is the negative half of the realize proof: fed a
// Source that does not exist, the walk must FAIL rather than silently land nothing. This is the
// assertion that would catch a `walkFile` that swallowed its read error.
func TestTheSecretStepRefusesAnUnreadableSource(t *testing.T) {
	// The source EXISTS at compile time (or the compile would refuse a required entry), and is
	// removed before the walk reads it: the residual race named in spec#363.
	src := filepath.Join(t.TempDir(), "present")
	if err := os.WriteFile(src, []byte("bytes"), 0o600); err != nil {
		t.Fatalf("writing the source: %v", err)
	}
	steps, err := deploykit.CompileSecretSteps(secretCandy(t, `
use:
  secret:
    lab-signing-key:
      src: `+src+`
      dst: /run/lab/id_ed25519
`))
	if err != nil {
		t.Fatalf("CompileSecretSteps: %v", err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatalf("removing the source: %v", err)
	}
	exec := newRecordingExecutor()
	if _, err := kit.WalkPlans(context.Background(), exec, []spec.InstallPlanView{{Steps: spec.StepsToView(steps)}}, kit.WalkOpts{}); err == nil {
		t.Fatal("the walk accepted an unreadable source and reported success — the deployment starts without the key it needs")
	}
	if len(exec.puts) != 0 {
		t.Fatalf("a failed read must not have written anything, got %v", keysOf(exec.puts))
	}
}

func keysOf(m map[string]recFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
