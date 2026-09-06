package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestDeployTargetEntity gates the ONE deploy-hop chain resolver (Phase 3): a plain kind:vm
// entity passes through; a kind:check BED (the clone-base deploy) resolves its from: chain to
// the terminal template. Removing the Fleet hop fails the bed case.
func TestDeployTargetEntity(t *testing.T) {
	uf := &spec.UnifiedFile{
		Fleet: map[string]spec.FleetNode{
			"check-vm-clone-base": {From: "cachyos-vm"},
		},
		PluginKinds: map[string]map[string]json.RawMessage{
			"vm": {"cachyos-vm": json.RawMessage("{}")},
		},
	}
	if got, ok := DeployTargetEntity(uf, "cachyos-vm"); !ok || got != "cachyos-vm" {
		t.Fatalf("plain entity: got (%q, %v), want (cachyos-vm, true)", got, ok)
	}
	if got, ok := DeployTargetEntity(uf, "check-vm-clone-base"); !ok || got != "cachyos-vm" {
		t.Fatalf("deploy hop: got (%q, %v), want (cachyos-vm, true)", got, ok)
	}
	if _, ok := DeployTargetEntity(uf, "no-such"); ok {
		t.Fatal("an unknown name must not resolve")
	}
}
