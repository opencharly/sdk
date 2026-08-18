package vmshared

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// The fifth silent drop: sshUnitForDistro and distroInit are bare table lookups, so an id the
// vocabulary does not carry yields "" — which renders `systemctl enable --now ` with no unit, or
// takes the systemd arm on an OpenRC guest. Either way the guest boots unreachable and says nothing.
func TestValidateDistroVocabulary_RejectsAnIdOutsideTheVocabulary(t *testing.T) {
	err := validateDistroVocabulary("gentoo")
	if err == nil {
		t.Fatal("an id outside the vocabulary was accepted; the sshd unit would render EMPTY and the " +
			"guest would boot unreachable with nothing in the output naming the cause")
	}
	// The message has to name the id and the alternatives, or the operator is no better off than
	// with the silent "" it replaces.
	for _, want := range []string{"gentoo", "valid ids are"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Presence control: EVERY id the generated vocabulary carries must pass. Without this, a guard that
// rejected everything would satisfy the test above and break every VM.
func TestValidateDistroVocabulary_AcceptsEveryVocabularyId(t *testing.T) {
	if len(spec.DistroIDs) == 0 {
		t.Fatal("spec.DistroIDs is empty — every assertion here would pass vacuously")
	}
	for _, id := range spec.DistroIDs {
		if err := validateDistroVocabulary(id); err != nil {
			t.Errorf("vocabulary id %q was rejected: %v", id, err)
		}
	}
}

// Absence is OpValidate's business, not the renderer's: an empty distro must not be turned into a
// render error here, or every pre-cutover spec stops rendering.
func TestValidateDistroVocabulary_EmptyIsNotThisGuardsProblem(t *testing.T) {
	if err := validateDistroVocabulary(""); err != nil {
		t.Errorf("an empty distro was rejected by the RENDERER; presence is enforced at author time "+
			"by the vm kind's OpValidate: %v", err)
	}
}

// The guard is wired into the entry point, not merely defined. A helper nothing calls is the
// silent drop wearing a different hat.
func TestRenderCloudInit_RejectsAnIdOutsideTheVocabulary(t *testing.T) {
	s := &VmSpec{Source: VmSource{Kind: "cloud_image", BaseUser: "x", Distro: "gentoo"}}
	_, _, _, err := RenderCloudInit(s, CloudInitRuntimeParams{Hostname: "h"})
	if err == nil {
		t.Fatal("RenderCloudInit accepted an out-of-vocabulary distro — the guard exists but is not wired in")
	}
	if !strings.Contains(err.Error(), "gentoo") {
		t.Errorf("RenderCloudInit error does not name the offending id: %v", err)
	}
}
