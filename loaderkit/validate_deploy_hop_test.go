package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestValidateCheckBedsAcceptsDeployHop gates the Phase 3 deploy-hop acceptance: a
// check bed's from: may name ANOTHER check bed (the clone-base bed - plugin-vm's
// drive resolves the chain at build time). Removing the uf.CheckBeds() membership
// check FAILS this test (the hop bed must not be rejected).
func TestValidateCheckBedsAcceptsDeployHop(t *testing.T) {
	threaded := spec.Threaded{
		DeployTraits: map[string]*spec.DeployTraits{
			"vm": {Venue: "ssh", BedTarget: true},
		},
	}
	disp := true
	hop := spec.FleetNode{Target: "vm", From: "check-vm-clone-base", Disposable: &disp}
	base := spec.FleetNode{Target: "vm", From: "cachyos-vm", Disposable: &disp}
	uf := &spec.UnifiedFile{
		Fleet: map[string]spec.FleetNode{
			"check-vm-clone":      hop,
			"check-vm-clone-base": base,
		},
		PluginKinds: map[string]map[string]json.RawMessage{
			"vm": {"cachyos-vm": json.RawMessage("{}")},
		},
	}
	if err := ValidateCheckBeds(uf, threaded); err != nil {
		t.Fatalf("the deploy-hop from: must be accepted (base bed name in CheckBeds): %v", err)
	}
	ufBad := &spec.UnifiedFile{
		Fleet: map[string]spec.FleetNode{
			"check-vm-clone": {Target: "vm", From: "no-such-entity-or-bed", Disposable: &disp},
		},
	}
	if err := ValidateCheckBeds(ufBad, threaded); err == nil {
		t.Fatal("an undefined from: must still be rejected")
	}
}
