package deploykit

// compile_secret_steps_test.go — the §8 secret DELIVERY, both halves.
//
// The first four tests exercise the compiler directly. The LAST one is the wiring proof: without
// the slot in `BuildDeployPlan` the step never reaches a plan and no file is ever delivered, which
// is exactly the failure mode a unit test on the compiler alone cannot see.
//
// The values in these fixtures are HOST PATHS, and no test asserts on bytes: the contract's whole
// point (D-SEC-1) is that the step carries `{Source, Dest, Mode}` and nothing that could hold the
// value. `kit.walkFile` is what reads the source at realize.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/opencharly/spec/spec"
)

// candyWithUse builds a CandyModel from an authored `use:` block, through the same adapter
// (NewSpecCandyModel) that charly's scanner installs for every candy.
func candyWithUse(t *testing.T, useYAML string) CandyModel {
	t.Helper()
	var m spec.CandyModel
	if err := yaml.Unmarshal([]byte(useYAML), &m); err != nil {
		t.Fatalf("decoding the authored use: block: %v", err)
	}
	return testCandy("lab-signing", m, spec.CandyView{})
}

// hostSecret writes a readable host file and returns its absolute path.
func hostSecret(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(p, []byte("PRIVATE-KEY-MATERIAL"), 0o600); err != nil {
		t.Fatalf("writing the host secret: %v", err)
	}
	return p
}

// fileSteps returns the plan's File steps.
func fileSteps(steps []InstallStep) []*FileStep {
	var out []*FileStep
	for _, s := range steps {
		if fs, ok := s.(*FileStep); ok {
			out = append(out, fs)
		}
	}
	return out
}

// TestCompileSecretSteps_EmitsOneFileStepPerEntrySorted: one step per entry, in NAME order so a
// plan is reproducible, each carrying the two PATHS and mode 0600.
func TestCompileSecretSteps_EmitsOneFileStepPerEntrySorted(t *testing.T) {
	src := hostSecret(t)
	candy := candyWithUse(t, `
use:
  secret:
    zzz-last:
      src: `+src+`
      dst: /run/lab/zzz
    aaa-first:
      src: `+src+`
      dst: /run/lab/aaa
`)
	steps, err := CompileSecretSteps(candy)
	if err != nil {
		t.Fatalf("CompileSecretSteps: %v", err)
	}
	got := fileSteps(steps)
	if len(got) != 2 {
		t.Fatalf("got %d file steps, want 2 (one per `use: secret:` entry): %#v", len(got), got)
	}
	// The NAMES are not part of the step, so the order is what pins the sort: aaa before zzz.
	if got[0].Dest != "/run/lab/aaa" || got[1].Dest != "/run/lab/zzz" {
		t.Fatalf("steps are not in entry-NAME order: %q then %q", got[0].Dest, got[1].Dest)
	}
	for _, s := range got {
		if s.Source != src {
			t.Errorf("step source = %q, want the host path %q", s.Source, src)
		}
		if s.Mode != 0o600 {
			t.Errorf("step mode = %#o, want 0600 — a secret delivered world-readable is not delivered", s.Mode)
		}
		if s.CandyName != "lab-signing" {
			t.Errorf("step candy = %q, want lab-signing (the ledger's key)", s.CandyName)
		}
	}
}

// TestCompileSecretSteps_OptionalMissingSourceSkips: DESIGN.md:735 — "the key is readable when the
// host had one". An optional entry whose source is absent delivers nothing and fails nothing.
func TestCompileSecretSteps_OptionalMissingSourceSkips(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	candy := candyWithUse(t, `
use:
  secret:
    lab-signing-key:
      src: `+missing+`
      dst: /run/lab/id_ed25519
      optional: true
`)
	steps, err := CompileSecretSteps(candy)
	if err != nil {
		t.Fatalf("an optional entry with a missing source must not fail the deployment: %v", err)
	}
	if len(fileSteps(steps)) != 0 {
		t.Fatalf("an optional entry with a missing source must deliver nothing, got %#v", fileSteps(steps))
	}
}

// TestCompileSecretSteps_RequiredMissingSourceErrors: the other half — a typo in `src` on a
// REQUIRED entry must not silently ship nothing.
func TestCompileSecretSteps_RequiredMissingSourceErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	candy := candyWithUse(t, `
use:
  secret:
    lab-signing-key:
      src: `+missing+`
      dst: /run/lab/id_ed25519
`)
	_, err := CompileSecretSteps(candy)
	if err == nil {
		t.Fatal("a required entry with an unreadable source must fail the compile")
	}
	for _, want := range []string{"lab-signing", "lab-signing-key", missing} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q — a reader must be able to find the entry", err, want)
		}
	}
}

// TestCompileSecretSteps_EntryWithoutDstErrors: a secret `use:` without both paths cannot be
// delivered, and saying so at compile is cheaper than a deploy that silently skips it.
func TestCompileSecretSteps_EntryWithoutDstErrors(t *testing.T) {
	src := hostSecret(t)
	candy := candyWithUse(t, `
use:
  secret:
    lab-signing-key:
      src: `+src+`
`)
	_, err := CompileSecretSteps(candy)
	if err == nil {
		t.Fatal("an entry without `dst` must fail the compile")
	}
	if !strings.Contains(err.Error(), "dst") {
		t.Fatalf("error %q does not name the missing `dst`", err)
	}
}

// TestBuildDeployPlan_CarriesTheSecretDeliveryStep is the WIRING half: the compiler is useless if
// `BuildDeployPlan` never calls it. Planted evidence: deleting the `CompileSecretSteps` call from
// install_build.go makes this test report zero file steps.
func TestBuildDeployPlan_CarriesTheSecretDeliveryStep(t *testing.T) {
	src := hostSecret(t)
	candy := candyWithUse(t, `
use:
  secret:
    lab-signing-key:
      src: `+src+`
      dst: /run/lab/id_ed25519
`)
	ctx, ex := testExecutor(nil)
	plan, err := BuildDeployPlan(ctx, ex, candy, testResolvedBox(), HostContext{})
	if err != nil {
		t.Fatalf("BuildDeployPlan: %v", err)
	}
	got := fileSteps(plan.Steps)
	if len(got) != 1 {
		t.Fatalf("the plan carries %d file steps, want 1 — the secret delivery never reached the plan", len(got))
	}
	if got[0].Dest != "/run/lab/id_ed25519" || got[0].Source != src {
		t.Fatalf("the delivered step carries %q -> %q, want %q -> %q", got[0].Source, got[0].Dest, src, "/run/lab/id_ed25519")
	}
	// Ordering: the delivery precedes every step the candy's own plan contributes, so a task that
	// reads `dst` (DESIGN.md:735's check) finds the file already in place.
	fileAt, opAt := -1, -1
	for i, s := range plan.Steps {
		if _, ok := s.(*FileStep); ok && fileAt < 0 {
			fileAt = i
		}
		if s.Kind() == StepKindOp && opAt < 0 {
			opAt = i
		}
	}
	if fileAt < 0 {
		t.Fatal("no FileStep in the plan")
	}
	if opAt >= 0 && fileAt > opAt {
		t.Fatalf("the secret delivery is at step %d, after the first op step at %d — a task would read `dst` before it lands", fileAt, opAt)
	}
}
