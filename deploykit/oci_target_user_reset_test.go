package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestOCITarget_ResetsUserBetweenCandies is the regression guard for charly#467.
//
// The overlay walker splices each step's Containerfile fragment VERBATIM, so a candy whose
// fragment legitimately switches to the image user leaves the build in that user — and every
// LATER candy's root work then runs unprivileged. Reproduced live before this fix: an overlay
// composing plugin-example-deploy (which emits `USER 1000`) ahead of the virtualization candy
// executed
//
//	[20/20] STEP  2/17: USER root        <- the overlay's own reset, after FROM
//	[20/20] STEP  8/17: USER 1000        <- spliced from plugin-example-deploy
//	[20/20] STEP 11/17: RUN ... pacman-key --init ...
//	==> ERROR: pacman-key needs to be run as root for this operation.
//
// with no reset between steps 8 and 11. The full-image path does not have this bug because
// Generator.WriteCandySteps tracks the user itself and resets between candies.
func TestOCITarget_ResetsUserBetweenCandies(t *testing.T) {
	tgt := &OCITarget{
		EmitStepOp: func(step spec.InstallStep, plan *spec.InstallPlan, distros []string) (string, error) {
			// The first candy's step switches to the image user, as an inline builder or a
			// user-scoped install legitimately does.
			if plan.Candy == "user-mode-candy" {
				return "USER 1000\nRUN mkdir -p /tmp/x\n", nil
			}
			return "RUN pacman-key --init\n", nil
		},
	}
	plans := []*spec.InstallPlan{
		{Candy: "user-mode-candy", Steps: []spec.InstallStep{&stubStep{kind: "File"}}},
		{Candy: "needs-root-candy", Steps: []spec.InstallStep{&stubStep{kind: "File"}}},
	}
	if err := tgt.Emit(plans, spec.EmitOpts{}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := tgt.String()

	userIdx := strings.Index(got, "USER 1000")
	rootIdx := strings.Index(got, "USER root")
	pacmanIdx := strings.Index(got, "pacman-key --init")
	if userIdx < 0 || pacmanIdx < 0 {
		t.Fatalf("fixture did not emit the expected directives:\n%s", got)
	}
	if rootIdx < 0 {
		t.Fatalf("no USER root reset emitted between candies; the root step would run as UID 1000:\n%s", got)
	}
	if !(userIdx < rootIdx && rootIdx < pacmanIdx) {
		t.Errorf("USER root must sit between the user switch and the next candy's root step "+
			"(userIdx=%d rootIdx=%d pacmanIdx=%d):\n%s", userIdx, rootIdx, pacmanIdx, got)
	}
}

// A candy that never leaves root must not provoke a redundant reset — the entry state is
// already root, so emitting one would be noise in every overlay.
func TestOCITarget_NoRedundantRootResetWhenAlreadyRoot(t *testing.T) {
	tgt := &OCITarget{
		EmitStepOp: func(step spec.InstallStep, plan *spec.InstallPlan, distros []string) (string, error) {
			return "RUN echo hi\n", nil
		},
	}
	plans := []*spec.InstallPlan{
		{Candy: "a", Steps: []spec.InstallStep{&stubStep{kind: "File"}}},
		{Candy: "b", Steps: []spec.InstallStep{&stubStep{kind: "File"}}},
	}
	if err := tgt.Emit(plans, spec.EmitOpts{}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if strings.Contains(tgt.String(), "USER root") {
		t.Errorf("emitted a USER root reset when the build never left root:\n%s", tgt.String())
	}
}

func TestLastUserDirective(t *testing.T) {
	cases := []struct{ frag, want string }{
		{"RUN echo hi\n", ""},
		{"USER 1000\nRUN x\n", "1000"},
		{"USER 1000\nRUN x\nUSER root\n", "root"},
		{"  USER  user \nRUN x\n", "user"},
		{"RUN echo 'USER 1000'\n", ""}, // not at line start: not a directive
	}
	for _, c := range cases {
		if got := lastUserDirective(c.frag); got != c.want {
			t.Errorf("lastUserDirective(%q) = %q, want %q", c.frag, got, c.want)
		}
	}
}
