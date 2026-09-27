package loaderkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// validate_check_beds_namespace_test.go — the namespaced-bed validation coverage.
//
// TRUE pre-change behavior: ValidateCheckBeds iterated uf.CheckBeds() (LOCAL-ONLY),
// so a namespaced bed was NEVER ENUMERATED — not rejected, simply never validated.
// The changed code iterates uf.Beds() (local + namespace-qualified) and validates
// each bed's from: against its OWNING namespace (uf.BedScope).
//
// The test that FAILS WITHOUT the change is _NamespacedFromRejectedWhenUndefined:
// pre-change, the namespaced bed was never enumerated, so its undefined from:
// produced NO error (the test's `want rejection` assertion fails); post-change it
// is enumerated and rejected. _NamespacedFromResolvesInOwnScope is the companion
// regression guard (a valid namespaced bed must NOT be over-rejected).

// nsThreaded reports bed_target for vm so the bed-target arm runs (the substrate
// trait check is data-driven, so the fixture must declare the trait).
func nsThreaded() spec.Threaded {
	return spec.Threaded{DeployTraits: map[string]*spec.DeployTraits{
		"vm": {Venue: "ssh", BedTarget: true},
	}}
}

// TestValidateCheckBeds_NamespacedFromResolvesInOwnScope: a valid namespaced bed
// (bare from: resolving in its own namespace) must NOT be rejected — the regression
// guard against over-rejection once namespaced beds enter the enumeration.
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

// TestValidateCheckBeds_NamespacedFromRejectedWhenUndefined LOCKS the change: a
// namespaced bed whose from: names nothing in its own namespace is rejected. This
// FAILS before the change (the namespaced bed was not enumerated, so no error).
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
	err := ValidateCheckBeds(uf, spec.Threaded{})
	if err == nil {
		t.Fatal("namespaced bed with an unregistered target passed validation, want rejection")
	}
	if !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("rejection does not name the target arm: %v", err)
	}
}

// TestValidateCheckBeds_NamespacedIterateAgentScoped LOCKS the owning-scope agent
// catalog read: an `omarchy` namespace carries an `agent:` catalog entry `claude`
// and an iterate bed referencing it. The catalog resolves in the bed's OWN
// namespace, so validation passes. FAILS before the change (the agent catalog was
// read from the merged ROOT, where `claude` is undefined).
func TestValidateCheckBeds_NamespacedIterateAgentScoped(t *testing.T) {
	disp := true
	ns := &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{
			"agent": {"claude": json.RawMessage(`{"agent":{"command":["claude"]}}`)},
		},
		Deploy: map[string]spec.DeployNode{
			"preflight": {
				Target:     "pod",
				Image:      "x",
				Disposable: &disp,
				Iterate:    &spec.Iterate{Sandbox: "some-sandbox", Agent: []string{"claude"}},
				Plan:       []spec.Step{{Check: "a scored check"}},
			},
		},
	}
	uf := &spec.UnifiedFile{Namespaces: map[string]*spec.UnifiedFile{"omarchy": ns}}
	if err := ValidateCheckBeds(uf, spec.Threaded{}); err != nil {
		t.Fatalf("namespaced iterate bed's own-namespace agent catalog not honored: %v", err)
	}
}
