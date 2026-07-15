package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// oci_target_test.go — the deploykit.OCITarget WALKER test. The walker is the kind-blind render
// M-mechanism relocated from charly core (P11c): it writes the `# Layer: <candy>` header, resolves
// the deferred {{.Home}} token, and delegates each step's fragment to the EmitStepOp seam (the host
// ociEmitStep dispatch, wired by the candy via HostBuild("step-emit","oci-emit-step")). This test
// exercises the walker in isolation with a MOCK EmitStepOp — proving the header + the home
// resolution + the per-step delegation + the VenueSkip/empty-fragment elision, independent of the
// host dispatch (which has its own core tests via the ociTestTarget helper).

func TestOCITarget_EmitWritesLayerHeaderAndDelegates(t *testing.T) {
	var calls []string
	tgt := &OCITarget{
		Home:    "/home/u",
		Distros: []string{"fedora"},
		EmitStepOp: func(step spec.InstallStep, plan *spec.InstallPlan, distros []string) (string, error) {
			calls = append(calls, string(step.Kind()))
			return "RUN echo " + string(step.Kind()) + "\n", nil
		},
	}
	plan := &spec.InstallPlan{Candy: "demo", Steps: []spec.InstallStep{
		&stubStep{kind: "File"},
		&stubStep{kind: "ShellHook"},
	}}
	if err := tgt.Emit([]*spec.InstallPlan{plan}, spec.EmitOpts{}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := tgt.String()
	if !strings.Contains(got, "# Layer: demo") {
		t.Errorf("missing # Layer header: %s", got)
	}
	if !strings.Contains(got, "RUN echo File") || !strings.Contains(got, "RUN echo ShellHook") {
		t.Errorf("step fragments not delegated/spliced: %s", got)
	}
	if len(calls) != 2 || calls[0] != "File" || calls[1] != "ShellHook" {
		t.Errorf("EmitStepOp calls = %v, want [File ShellHook]", calls)
	}
}

func TestOCITarget_EmitElidesVenueSkipAndEmptyFragments(t *testing.T) {
	tgt := &OCITarget{
		EmitStepOp: func(step spec.InstallStep, plan *spec.InstallPlan, distros []string) (string, error) {
			if step.Kind() == "Empty" {
				return "", nil // a deploy-only / no-op step
			}
			return "RUN x\n", nil
		},
	}
	plan := &spec.InstallPlan{Candy: "x", Steps: []spec.InstallStep{
		&stubStep{kind: "Skip", venue: spec.VenueSkip},
		&stubStep{kind: "Empty"},
		&stubStep{kind: "Real"},
	}}
	if err := tgt.Emit([]*spec.InstallPlan{plan}, spec.EmitOpts{}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := tgt.String()
	if strings.Contains(got, "Skip") {
		t.Errorf("VenueSkip step was rendered: %s", got)
	}
	if strings.Contains(got, "Empty") {
		t.Errorf("empty-fragment step was rendered: %s", got)
	}
	if !strings.Contains(got, "RUN x") {
		t.Errorf("real step fragment missing: %s", got)
	}
}

func TestOCITarget_EmitNilEmitStepOpIsNoOp(t *testing.T) {
	// A nil EmitStepOp (no seam wired) → the walker writes the header + home resolution only;
	// each step's emitStep returns "" (no fragment). Used by tests that exercise only the walker.
	tgt := &OCITarget{Home: "/home/u"}
	plan := &spec.InstallPlan{Candy: "solo", Steps: []spec.InstallStep{
		&stubStep{kind: "File"},
	}}
	if err := tgt.Emit([]*spec.InstallPlan{plan}, spec.EmitOpts{}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := tgt.String()
	if !strings.Contains(got, "# Layer: solo") {
		t.Errorf("missing header: %s", got)
	}
	if strings.Contains(got, "File") {
		t.Errorf("nil seam should emit no step fragment: %s", got)
	}
}

// stubStep is a synthetic InstallStep for walker tests.
type stubStep struct {
	kind  string
	venue spec.Venue
}

func (s *stubStep) Kind() spec.StepKind       { return spec.StepKind(s.kind) }
func (s *stubStep) Scope() spec.Scope         { return spec.ScopeUser }
func (s *stubStep) Venue() spec.Venue         { return s.venue }
func (s *stubStep) RequiresGate() spec.Gate   { return spec.GateNone }
func (s *stubStep) Reverse() []spec.ReverseOp { return nil }
