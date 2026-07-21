package kit

import (
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestBedVmDomains proves the per-DEPLOY domain serialization key-gathering (P33): a bed's VM
// domains are collected from its OWN vm target (keyed by the BED NAME) AND from any group-member
// vm targets (keyed by the MEMBER KEY), deduped + sorted. Because the domain is keyed by the
// DEPLOY — NOT the shared kind:vm entity — two DISTINCT beds referencing one entity resolve to
// DISTINCT domains and so hold distinct locks and run concurrently (the collision-free-by-
// construction goal). The lock only serializes two invocations of the SAME deploy on its own
// domain. Moved from charly/bed_vm_domain_test.go (CHECK-wave bed-session spike) — this package's
// BedVmDomains reads node.Descent directly (no registry fallback for a synthetic node), so every
// fixture here stamps Descent explicitly, exactly as the loader's StampDescent would for the real
// compiled-in substrate kinds (vm: Venue "ssh"; pod/group: no ssh venue).
func TestBedVmDomains(t *testing.T) {
	vmDescent := &spec.DescentDescriptor{Venue: "ssh"}
	podDescent := &spec.DescentDescriptor{Venue: "container"}

	// Direct vm bed: the domain is charly-<bed-name>, NOT charly-<entity>.
	if got := BedVmDomains("check-k3s-vm", spec.BundleNode{Target: "vm", From: "k3s-vm", Descent: vmDescent}); !reflect.DeepEqual(got, []string{"charly-check-k3s-vm"}) {
		t.Fatalf("direct vm bed: got %v, want [charly-check-k3s-vm]", got)
	}
	// A SIBLING bed sharing the SAME entity resolves to a DIFFERENT domain — the P33 property that
	// makes them collision-free (pre-P33 both were charly-k3s-vm and serialized).
	if got := BedVmDomains("check-substrate", spec.BundleNode{Target: "vm", From: "k3s-vm", Descent: vmDescent}); !reflect.DeepEqual(got, []string{"charly-check-substrate"}) {
		t.Fatalf("sibling vm bed sharing the entity: got %v, want [charly-check-substrate] (distinct domain)", got)
	}
	// Group with a vm member: the member's domain is keyed by the MEMBER KEY.
	group := spec.BundleNode{Target: "group", Members: map[string]*spec.BundleNode{
		"check-k8s-deploy-cluster":  {Target: "vm", From: "k3s-vm", Descent: vmDescent},
		"check-k8s-deploy-workload": {Target: "k8s", Descent: &spec.DescentDescriptor{LeafOnly: true}},
	}}
	if got := BedVmDomains("check-k8s-deploy", group); !reflect.DeepEqual(got, []string{"charly-check-k8s-deploy-cluster"}) {
		t.Fatalf("group with a vm member: got %v, want [charly-check-k8s-deploy-cluster] (member-key domain)", got)
	}
	if got := BedVmDomains("check-pod", spec.BundleNode{Target: "pod", Descent: podDescent}); len(got) != 0 {
		t.Fatalf("non-vm bed: got %v, want no domains", got)
	}
	// A multi-vm group's distinct member domains come back sorted + deduped (dup member keys can't
	// occur in a map, so dedup here guards the root+member overlap path).
	multi := spec.BundleNode{Target: "group", Members: map[string]*spec.BundleNode{
		"member-b": {Target: "vm", From: "shared-entity", Descent: vmDescent},
		"member-a": {Target: "vm", From: "shared-entity", Descent: vmDescent},
	}}
	if got := BedVmDomains("multi", multi); !reflect.DeepEqual(got, []string{"charly-member-a", "charly-member-b"}) {
		t.Fatalf("multi-vm group: got %v, want sorted [charly-member-a charly-member-b]", got)
	}
}
