package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestBuildFleetNode_FromNameTag is the regression guard for the unified from: name:tag
// syntax (Cutover A addendum — the pod/VM unification): a backing-chain substrate (vm)
// accepts `from: base:snapshot` as the unified spelling of `from: base` +
// `from_snapshot: snapshot` — the SAME name:tag shape pods use (`from: image:tag`).
func TestBuildFleetNode_FromNameTag(t *testing.T) {
	th := threadedWithVM()
	pn := spec.ParsedNode{Name: "check-instrument-omarchy-vm", Disc: "vm", Body: spec.RawBody(`"omarchy-vm:golden"`)}
	dn, err := BuildFleetNode(pn, th)
	if err != nil {
		t.Fatalf("BuildFleetNode: %v", err)
	}
	if dn.From != "omarchy-vm" {
		t.Errorf("From = %q, want omarchy-vm", dn.From)
	}
	if dn.FromSnapshot != "golden" {
		t.Errorf("FromSnapshot = %q, want golden", dn.FromSnapshot)
	}
	if dn.Target != "vm" {
		t.Errorf("Target = %q, want vm", dn.Target)
	}
}

// TestBuildFleetNode_FromNameNoTag proves a plain `from: name` (no tag) is unchanged.
func TestBuildFleetNode_FromNameNoTag(t *testing.T) {
	th := threadedWithVM()
	pn := spec.ParsedNode{Name: "check-omarchy-vm", Disc: "vm", Body: spec.RawBody(`"omarchy-vm"`)}
	dn, err := BuildFleetNode(pn, th)
	if err != nil {
		t.Fatalf("BuildFleetNode: %v", err)
	}
	if dn.From != "omarchy-vm" {
		t.Errorf("From = %q, want omarchy-vm", dn.From)
	}
	if dn.FromSnapshot != "" {
		t.Errorf("FromSnapshot = %q, want empty", dn.FromSnapshot)
	}
}

// TestBuildFleetNode_FromNameTagDualSpellingConflict proves a contradictory dual spelling
// (from: name:tag AND from_snapshot: with a different value) is a loud authoring error.
func TestBuildFleetNode_FromNameTagDualSpellingConflict(t *testing.T) {
	th := threadedWithVM()
	pn := spec.ParsedNode{Name: "check-instrument-omarchy-vm", Disc: "vm", Body: spec.RawBody(`{"from":"omarchy-vm:golden","from_snapshot":"other"}`)}
	_, err := BuildFleetNode(pn, th)
	if err == nil {
		t.Fatal("BuildFleetNode: nil error, want dual-spelling conflict error")
	}
}

// TestBuildFleetNode_FromNameTagPodUnchanged proves a pod deploy's `from: image:tag` is
// NOT split (pods have no backing chains — the tag is part of the image ref).
func TestBuildFleetNode_FromNameTagPodUnchanged(t *testing.T) {
	th := spec.Threaded{
		DeployTraits: map[string]*spec.DeployTraits{
			"pod": {Venue: "container", ImageBacked: true, ImageContext: true},
		},
	}
	pn := spec.ParsedNode{Name: "my-pod", Disc: "pod", Body: spec.RawBody(`"ghcr.io/opencharly/my-image:latest"`)}
	dn, err := BuildFleetNode(pn, th)
	if err != nil {
		t.Fatalf("BuildFleetNode: %v", err)
	}
	if dn.Image != "ghcr.io/opencharly/my-image:latest" {
		t.Errorf("Image = %q, want the full ref with tag", dn.Image)
	}
	if dn.FromSnapshot != "" {
		t.Errorf("FromSnapshot = %q, want empty (pods have no backing chains)", dn.FromSnapshot)
	}
}

// threadedWithVM returns a Threaded snapshot with the vm substrate declared (the canonical
// vm traits: ssh venue, machine venue, exclusive venue, supports_from_snapshot).
func threadedWithVM() spec.Threaded {
	return spec.Threaded{
		DeployTraits: map[string]*spec.DeployTraits{
			"vm": {Venue: "ssh", MachineVenue: true, ExclusiveVenue: true, SupportsFromSnapshot: true},
		},
	}
}
