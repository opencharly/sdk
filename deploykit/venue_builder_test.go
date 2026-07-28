package deploykit

import (
	"context"
	"strings"
	"testing"
)

// venue_builder_test.go — ported from charly/builder_venue_test.go and
// charly/vm_builder_xhost_test.go (#118, coneB-p8bremainder): venueBuilderTarName,
// BuilderStepImage, and RunVenueBuilderStep's unknown-builder routing moved here with the rest
// of the venue-builder orchestration, so their coverage moves with it. The ONE sibling test that
// stayed in charly (TestRunVenueBuilderStepRoutesHomeBuilders) needs a REAL project-loaded
// BuilderDef fixture (LoadBuildConfigForBox) — a genuine core (project-loader) dependency this
// package cannot replicate — so it exercises deploykit.RunVenueBuilderStep from charly instead.

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
