package kit

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestClassifyContainerInfraFailure drives the named signature table (the R44 mandate:
// "the regression test drives the table"). It proves the DISCRIMINATOR that makes the fix
// safe: a podman container-SETUP failure (the verbatim R44 error, exit-125, the store-churn
// signatures) is classified INFRA, while a GENUINE in-container "command not found" (bash
// ran, the command is absent) is NOT — it stays an ordinary check result. Without the fix
// (no classifier), an infra exit-127 was laundered into a check FAIL; this table is what
// tells the two apart.
func TestClassifyContainerInfraFailure(t *testing.T) {
	// The verbatim R44 error (from the p11ab check-sidecar-pod/check-image.log evidence).
	const r44Stderr = "Error: creating temporary passwd file for container " +
		"abc123: container abc123: ...\nCleaning up container: unmounting container abc123"

	infra := []struct {
		name   string
		exit   int
		stderr string
	}{
		{"r44 passwd-file setup race (exit 127)", 127, r44Stderr},
		{"podman own error exit 125 (any stderr)", 125, "whatever"},
		{"unmounting-container cleanup", 137, "Cleaning up container: unmounting container xyz"},
		{"error creating container", 126, "Error: error creating container storage: ..."},
		{"overlay mount failure", 127, "Error: error mounting storage for container: ..."},
		{"layer not known (build+prune race)", 125, "layer not known"},
		{"image not known (prune between resolve+mount)", 127, "locating image with ID: image not known"},
		{"store sqlite write-lock", 125, "Error: beginning transaction: database is locked"},
		{"OCI runtime could not exec", 127, "Error: OCI runtime attempted to invoke a command that was not found"},
	}
	for _, tc := range infra {
		t.Run("infra/"+tc.name, func(t *testing.T) {
			sig, ok := ClassifyContainerInfraFailure(tc.exit, tc.stderr)
			if !ok {
				t.Fatalf("exit=%d stderr=%q: want INFRA classification, got none", tc.exit, tc.stderr)
			}
			if sig == "" {
				t.Errorf("classified infra but returned an empty signature (annotation must be non-empty)")
			}
		})
	}

	notInfra := []struct {
		name   string
		exit   int
		stderr string
	}{
		{"genuine in-container command not found (bash ran)", 127, "bash: line 1: nonesuch: command not found"},
		{"genuine check assertion fail (test -f, file absent)", 1, ""},
		{"a user command's non-125 nonzero exit", 3, "usage: ..."},
		{"zero exit is never infra even with a scary stderr", 0, "creating temporary passwd file"},
	}
	for _, tc := range notInfra {
		t.Run("not-infra/"+tc.name, func(t *testing.T) {
			if sig, ok := ClassifyContainerInfraFailure(tc.exit, tc.stderr); ok {
				t.Fatalf("exit=%d stderr=%q: want NOT-infra (an authoritative check result), got infra %q",
					tc.exit, tc.stderr, sig)
			}
		})
	}
}

// infraStubParent is a parent executor whose RunCapture returns canned (stdout,stderr,exit)
// — simulating a podman container-setup infra failure without a real container.
type infraStubParent struct {
	ShellExecutor
	stdout, stderr string
	exit           int
}

func (p *infraStubParent) RunCapture(_ context.Context, _ string) (string, string, int, error) {
	return p.stdout, p.stderr, p.exit, nil
}

// TestNestedExecutorRunCapture_ClassifiesContainerInfra proves the executor SEAM: for a
// CONTAINER jump, a podman infra failure surfaces as a MARKED error (which the verbs
// propagate via `execution error: %v` and the retry/exit-mapping key on); a genuine
// command-not-found surfaces as a plain (exit, nil) result; and a NON-container (SSH) jump
// is never re-typed. This is the point that FAILS pre-fix (RunCapture returned (127, nil)
// for the infra case) and PASSES post-fix.
func TestNestedExecutorRunCapture_ClassifiesContainerInfra(t *testing.T) {
	ctx := context.Background()

	t.Run("container jump + infra stderr -> marked error", func(t *testing.T) {
		p := &infraStubParent{stderr: "creating temporary passwd file for container c: ...", exit: 127}
		n := &NestedExecutor{Parent: p, Jump: NestedJump{Kind: JumpPodmanExec, Target: "charly-checkbox-1-1"}}
		_, _, exit, err := n.RunCapture(ctx, "true")
		if err == nil || !strings.Contains(err.Error(), ContainerInfraErrMarker) {
			t.Fatalf("infra container-run must return a marked error (%q); got exit=%d err=%v",
				ContainerInfraErrMarker, exit, err)
		}
	})

	t.Run("container jump + genuine command-not-found -> plain result (no marker)", func(t *testing.T) {
		p := &infraStubParent{stderr: "bash: line 1: foo: command not found", exit: 127}
		n := &NestedExecutor{Parent: p, Jump: NestedJump{Kind: JumpPodmanExec, Target: "charly-checkbox-1-1"}}
		_, _, exit, err := n.RunCapture(ctx, "true")
		if err != nil {
			t.Fatalf("a genuine in-container command-not-found is a CHECK result, not an infra error; got %v", err)
		}
		if exit != 127 {
			t.Errorf("exit should pass through unchanged; got %d", exit)
		}
	})

	t.Run("non-container (SSH) jump is never classified", func(t *testing.T) {
		p := &infraStubParent{stderr: "creating temporary passwd file for container c: ...", exit: 125}
		n := &NestedExecutor{Parent: p, Jump: NestedJump{Kind: JumpSSH, Target: "host"}}
		_, _, _, err := n.RunCapture(ctx, "true")
		if err != nil {
			t.Fatalf("an SSH-jump result must not be re-typed as a container-setup infra failure; got %v", err)
		}
	})
}

// TestRunWithEventually_DoesNotRetryContainerInfra proves the CLASSIFY-ONLY design (R44,
// team-lead ruling): a container-setup infra failure is deliberately NOT retried. Option A
// (the persistent-container box-mode) removes the O(N) setup contention at the root, so a
// residual setup failure is RARE and MEANINGFUL — a retry would ABSORB and hide it, but the
// concurrency mandate wants infra events LOUD. So the probe runs exactly once and keeps its
// infra marker (→ the infra exit class via failErrorFor). Only the signal-kill class is
// retried (unchanged — a separate infra INTERRUPTION where re-running is correct).
func TestRunWithEventually_DoesNotRetryContainerInfra(t *testing.T) {
	calls := 0
	got := RunWithEventually(context.Background(), &spec.Op{}, func() CheckResult {
		calls++
		return CheckResult{Status: StatusFail, Message: "execution error: " + ContainerInfraErrMarker +
			` ["creating temporary passwd file" (…)]: podman exit=127`}
	})
	if calls != 1 {
		t.Fatalf("a container-setup infra failure must NOT be retried (classify-only keeps residual infra events loud); got %d calls", calls)
	}
	if got.Status != StatusFail || !IsContainerInfraResult(got.Message) {
		t.Fatalf("the infra marker must survive to the exit mapping (→ infra exit class); status=%v msg=%q", got.Status, got.Message)
	}
}
