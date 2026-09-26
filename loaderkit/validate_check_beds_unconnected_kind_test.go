package loaderkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// validate_check_beds_unconnected_kind_test.go — regression for the R1 defect found while wiring
// the KubeVirt R10 beds: a kind:check bed whose `from:` names a template folded by a STRUCTURAL
// kind whose provider did not connect in THIS process (e.g. a `kindcluster:` bed whose template
// plugin-kube folds, absent from a read-only plugin-side load that has no connect pass)
// false-failed "not defined" — and because ValidateCheckBeds returns on the FIRST error, that ONE
// bed aborted validation for the WHOLE project, making EVERY vm bed un-runnable in such a process
// (measured: plugin-deploy-vm's prepare-venue re-load).
//
// The fix exempts a `from:` whose kind folded ZERO templates in this scope (the process-capability
// gap: the provider never ran here). A kind that folded SOME templates (but not this ref) is a
// real typo and stays rejected.

// structuralThreaded declares `kindcluster` a STRUCTURAL kind.
func structuralThreaded() spec.Threaded {
	return spec.Threaded{
		DeployTraits: map[string]*spec.DeployTraits{
			"vm":          {Venue: "ssh", BedTarget: true},
			"kindcluster": {Venue: "none", BedTarget: true},
		},
		StructuralKinds: map[string]bool{"kindcluster": true},
	}
}

// kindBedProject builds a project with a `kindcluster:` bed whose from: names the kindcluster
// template `check-kc-cluster`. `folded` controls whether the kindcluster template actually folded
// into this scope (the provider connected HERE) — the ONE variable the fix keys on.
func kindBedProject(folded bool) *spec.UnifiedFile {
	disp := true
	uf := &spec.UnifiedFile{
		Deploy: map[string]spec.DeployNode{
			"check-kc": {Target: "kindcluster", From: "check-kc-cluster", Disposable: &disp},
		},
	}
	if folded {
		uf.PluginKinds = map[string]map[string]json.RawMessage{
			"kindcluster": {"check-kc-cluster": json.RawMessage(`{"kindcluster":{"box":"","engine":"podman"}}`)},
		}
	}
	return uf
}

// TestValidateCheckBeds_UnconnectedKindFromExempt: with the kindcluster template NOT folded here
// (provider absent), its bed's from: must be exempt — it cannot resolve in a process that never
// ran the kind's provider. FAILS without the fix ("kindcluster entity … not defined").
func TestValidateCheckBeds_UnconnectedKindFromExempt(t *testing.T) {
	err := ValidateCheckBeds(kindBedProject(false), structuralThreaded())
	if err != nil {
		t.Fatalf("an un-folded structural kind's from: must be exempt, got: %v", err)
	}
}

// TestValidateCheckBeds_ConnectedKindFromResolves: with the template folded here, the from:
// resolves and validation passes.
func TestValidateCheckBeds_ConnectedKindFromResolves(t *testing.T) {
	if err := ValidateCheckBeds(kindBedProject(true), structuralThreaded()); err != nil {
		t.Fatalf("a folded structural kind's from: must resolve, got: %v", err)
	}
}

// TestValidateCheckBeds_ConnectedKindBadRefStillEnforced: once the kind folded SOME template, a
// from: that names nothing is a real typo and STILL rejected — the exemption is scoped to the
// zero-folded (provider-absent) case, never a blanket skip.
func TestValidateCheckBeds_ConnectedKindBadRefStillEnforced(t *testing.T) {
	disp := true
	folded := kindBedProject(true)
	folded.Deploy["check-kc"] = spec.DeployNode{Target: "kindcluster", From: "does-not-exist", Disposable: &disp}
	err := ValidateCheckBeds(folded, structuralThreaded())
	if err == nil || !strings.Contains(err.Error(), "which is not defined") {
		t.Fatalf("a folded kind's undefined from: must still be rejected, got: %v", err)
	}
}

// TestValidateCheckBeds_BuiltinKindUnaffected: a built-in resource kind (vm) is NEVER a
// structural-vocabulary word, so its from: is always enforced — the exemption cannot leak.
func TestValidateCheckBeds_BuiltinKindUnaffected(t *testing.T) {
	disp := true
	uf := &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{
		"bed": {Target: "vm", From: "nope", Disposable: &disp},
	}}
	err := ValidateCheckBeds(uf, structuralThreaded())
	if err == nil || !strings.Contains(err.Error(), "which is not defined") {
		t.Fatalf("a built-in vm bed with an undefined from: must be rejected, got: %v", err)
	}
}
