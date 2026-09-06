package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestBuildFleetNodeSplitsMappingFromSnapshot(t *testing.T) {
	threaded := spec.Threaded{
		DeployTraits: map[string]*spec.DeployTraits{
			"vm": {Venue: "ssh"},
		},
	}
	pn := spec.ParsedNode{
		Name: "cachyos-vm-deploy",
		Disc: "vm",
		Body: json.RawMessage(`{"from": "cachyos-vm:golden"}`),
	}
	dn, err := BuildFleetNode(pn, threaded)
	if err != nil {
		t.Fatalf("BuildFleetNode(mapping form): %v", err)
	}
	if dn.From != "cachyos-vm" {
		t.Errorf("dn.From = %q, want cachyos-vm", dn.From)
	}
	if dn.FromSnapshot != "golden" {
		t.Errorf("dn.FromSnapshot = %q, want golden (the mapping-form name:tag split is missing)", dn.FromSnapshot)
	}
}
