package loaderkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// validate_check_beds_namespace_test.go — the namespaced-bed validation coverage:
// ValidateCheckBeds iterates uf.Beds() (local + namespace-qualified) and validates
// each bed's `from:` against its OWNING namespace (uf.BedScope). FAILS without the
// scope change: a namespaced bed whose bare `from:` resolves only inside its own
// namespace was rejected when validated against the merged root.

// nsThreaded reports bed_target for vm so the bed-target arm runs (the substrate
// trait check is data-driven, so the fixture must declare the trait).
func nsThreaded() spec.Threaded {
	return spec.Threaded{DeployTraits: map[string]*spec.DeployTraits{
		"vm": {Venue: "ssh", BedTarget: true},
	}}
}

// TestValidateCheckBeds_NamespacedFromResolvesInOwnScope: an `omarchy` namespace
// carries a vm template `omarchy-vm` and a bed `edge-inst` with bare `from:
// omarchy-vm`. The bed's from: resolves ONLY within omarchy — validating it against
// the root (the pre-fix local-only path) rejects it; validating against the owning
// scope accepts it.
func TestValidateCheckBeds_NamespacedFromResolvesInOwnScope(t *testing.T) {
	disp := true
	vmBody := json.RawMessage(`{"vm":{"source":{"kind":"iso"}}}`)
	ns := &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{"vm": {"omarchy-vm": vmBody}},
		Deploy: map[string]spec.DeployNode{
			"edge-inst": {Target: "vm", From: "omarchy-vm", Disposable: &disp},
		},
	}
	uf := &spec.UnifiedFile{
		Namespaces: map[string]*spec.UnifiedFile{"omarchy": ns},
	}
	if err := ValidateCheckBeds(uf, nsThreaded()); err != nil {
		t.Fatalf("namespaced bed with a namespace-local from: rejected: %v", err)
	}
}

// TestValidateCheckBeds_NamespacedFromRejectedWhenUndefined: a namespaced bed whose
// from: names nothing in its own namespace is rejected (the gate still bites).
func TestValidateCheckBeds_NamespacedFromRejectedWhenUndefined(t *testing.T) {
	disp := true
	ns := &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{"vm": {"omarchy-vm": json.RawMessage("{}")}},
		Deploy: map[string]spec.DeployNode{
			"edge-inst": {Target: "vm", From: "does-not-exist", Disposable: &disp},
		},
	}
	uf := &spec.UnifiedFile{Namespaces: map[string]*spec.UnifiedFile{"omarchy": ns}}
	err := ValidateCheckBeds(uf, nsThreaded())
	if err == nil {
		t.Fatal("namespaced bed with an undefined from: accepted, want rejection")
	}
	if !strings.Contains(err.Error(), "which is not defined") {
		t.Fatalf("rejection does not name the missing template: %v", err)
	}
}

// TestValidateCheckBeds_NamespacedIsEnumerated: a namespaced bed IS part of
// uf.Beds(), so its substrate/target invariants are enforced even though it lives
// in an imported namespace — an unsupported target on a namespaced bed is rejected.
func TestValidateCheckBeds_NamespacedIsEnumerated(t *testing.T) {
	disp := true
	ns := &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{"vm": {"omarchy-vm": json.RawMessage("{}")}},
		Deploy: map[string]spec.DeployNode{
			"edge-inst": {Target: "vm", From: "omarchy-vm", Disposable: &disp},
		},
	}
	uf := &spec.UnifiedFile{Namespaces: map[string]*spec.UnifiedFile{"omarchy": ns}}
	// No vm trait registered → the vm target is "unsupported", proving the
	// namespaced bed reached the validator's target arm.
	err := ValidateCheckBeds(uf, spec.Threaded{})
	if err == nil {
		t.Fatal("namespaced bed with an unregistered target passed validation, want rejection")
	}
	if !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("rejection does not name the target arm: %v", err)
	}
}
