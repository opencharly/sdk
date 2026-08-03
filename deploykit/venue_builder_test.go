package deploykit

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// venue_builder_test.go — ported from charly/builder_venue_test.go and
// charly/vm_builder_xhost_test.go (#118, coneB-p8bremainder): venueBuilderTarName,
// BuilderStepImage, and RunVenueBuilderStep's unknown-builder routing moved here with the rest
// of the venue-builder orchestration, so their coverage moves with it. charly's sibling test
// (TestRunVenueBuilderStepRoutesHomeBuilders, vm_builder_xhost_test.go) still loads its
// BuilderDef fixtures from the REAL committed build.yml via LoadBuildConfigForBox — a genuine
// core (project-loader) dependency this package cannot replicate — but the ROUTING decision
// itself (LocalPkg nil + a non-empty phase.install.host cell → the home-artifact builder path,
// by OUTPUT SHAPE, never a hardcoded builder-name list) depends on nothing charly-specific: a
// literal *Builder with the SAME shape routes identically regardless of who produced it (#55
// final-tail split-by-assertion round, team-lead directive 2026-08-03 — see
// TestRunVenueBuilderStepRoutesHomeBuilders_LiteralFixture below, which proves that decision
// directly).

// noopImageSeams are the resolveImage/ensureImage closures for a test that never reaches the
// aur/LocalPkg branch (a pre-branch error, here).
func noopImageSeams() (func(string) (string, error), func(context.Context, string) error) {
	return func(string) (string, error) { return "", nil }, func(context.Context, string) error { return nil }
}

// TestResolveBuilderImage verifies builder-image resolution order — override > compiled step,
// hard-error when none resolves. BuilderStepImage is the venue-agnostic free helper shared by
// the VM target + the F3 build channel (R3).
func TestResolveBuilderImage(t *testing.T) {
	if img, _ := BuilderStepImage(&BuilderStep{Builder: "npm", BuilderImage: "from-step"}, EmitOpts{BuilderImageOverride: "from-override"}); img != "from-override" {
		t.Errorf("override should win, got %q", img)
	}
	if img, _ := BuilderStepImage(&BuilderStep{Builder: "npm", BuilderImage: "from-step"}, EmitOpts{}); img != "from-step" {
		t.Errorf("compiled step image should win, got %q", img)
	}
	if _, err := BuilderStepImage(&BuilderStep{Builder: "npm", CandyName: "claude-code"}, EmitOpts{}); err == nil {
		t.Error("no image resolvable → expected error")
	}
}

// TestRunVenueBuilderStepUnknown verifies a builder with no phase.install.host cell (no resolved
// vmshared.BuilderDef) honors --skip-incompatible, and hard-errors otherwise pointing at the
// missing host cell. Routing is by output shape (no LocalPkg → home-artifact path; no host cell
// there → unsupported), not a hardcoded builder-name list.
func TestRunVenueBuilderStepUnknown(t *testing.T) {
	s := &BuilderStep{Builder: "bogus", CandyName: "x"}
	resolveImage, ensureImage := noopImageSeams()

	if err := RunVenueBuilderStep(context.Background(), &localPkgRecExec{}, "", resolveImage, ensureImage, s, EmitOpts{SkipIncompatible: true}); err != nil {
		t.Errorf("unknown builder with --skip-incompatible should be skipped, got %v", err)
	}
	err := RunVenueBuilderStep(context.Background(), &localPkgRecExec{}, "", resolveImage, ensureImage, s, EmitOpts{})
	if err == nil || !strings.Contains(err.Error(), "phase.install.host") {
		t.Errorf("unknown builder without skip should error pointing at the missing host cell, got %v", err)
	}
}

// TestRunVenueBuilderStepRoutesHomeBuilders_LiteralFixture proves the D3 routing decision
// (npm/pixi/cargo-shaped builders — LocalPkg nil + a phase.install.host cell — route to the
// cross-host home-artifact builder, by OUTPUT SHAPE, never a hardcoded builder-name list) using
// LITERAL BuilderDef fixtures instead of the real committed build.yml (charly's sibling
// TestRunVenueBuilderStepRoutesHomeBuilders exercises the SAME routing against the real
// config-driven host cells — the two are complementary, not duplicates: this one isolates the
// routing decision itself). Verified via the dry-run path so no podman is spawned.
func TestRunVenueBuilderStepRoutesHomeBuilders_LiteralFixture(t *testing.T) {
	resolveImage, ensureImage := noopImageSeams()
	for _, name := range []string{"npm", "pixi", "cargo"} {
		s := &BuilderStep{
			Builder:      name,
			CandyName:    "x",
			CandyDir:     "/tmp/x",
			BuilderImage: "test-builder:latest",
			BuilderDef: &spec.Builder{
				Phases: &spec.PhaseSet{Install: &spec.PhaseTemplates{Host: "echo building " + name}},
			},
		}
		if err := RunVenueBuilderStep(context.Background(), &localPkgRecExec{}, "", resolveImage, ensureImage, s, EmitOpts{DryRun: true}); err != nil {
			t.Errorf("RunVenueBuilderStep(%s) dry-run routed to home-artifact builder errored: %v", name, err)
		}
	}
}

// TestVenueBuilderTarNameUniquePerScope verifies the venue transfer tarball name is unique per
// deploy invocation: two concurrent deploys of the same candy to the same venue must never share
// it (the second PutFile would overwrite the first's tarball before its extract runs).
func TestVenueBuilderTarNameUniquePerScope(t *testing.T) {
	a := venueBuilderTarName("mycandy", "charly-venue-builder-tar-aaa111")
	b := venueBuilderTarName("mycandy", "charly-venue-builder-tar-bbb222")
	if a == b {
		t.Fatalf("same candy in different scopes must not share a venue tarball name: %q", a)
	}
	if got := venueBuilderTarName("mycandy", "charly-venue-builder-tar-aaa111"); got != a {
		t.Fatalf("same scope must be stable: %q vs %q", got, a)
	}
	want := "/tmp/charly-builder-mycandy-charly-venue-builder-tar-aaa111.tar.gz"
	if a != want {
		t.Fatalf("unexpected name: got %q want %q", a, want)
	}
	if other := venueBuilderTarName("othercandy", "charly-venue-builder-tar-aaa111"); other == a {
		t.Fatalf("different candies must not share a venue tarball name: %q", other)
	}
}
