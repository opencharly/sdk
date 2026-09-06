package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestSplitVMSnapshotRef — the unified from: name:tag spelling splits on the LAST ':':
// a VM entity name may not contain ':', so the split is unambiguous; no ':' → plain name.
func TestSplitVMSnapshotRef(t *testing.T) {
	cases := []struct{ ref, name, snap string }{
		{"base:golden", "base", "golden"},
		{"base", "base", ""},
		{"a:b:c", "a:b", "c"}, // last-colon split: the entity is a:b, the snapshot c
	}
	for _, c := range cases {
		name, snap := splitVMSnapshotRef(c.ref)
		if name != c.name || snap != c.snap {
			t.Errorf("splitVMSnapshotRef(%q) = (%q, %q), want (%q, %q)", c.ref, name, snap, c.name, c.snap)
		}
	}
}

// TestSetFleetCrossRefSplitsVMSnapshot — the scalar deploy form: a vm deploy's cross-ref
// base:golden splits into From+FromSnapshot; an image-backed pod ref (whose tags
// legitimately contain ':') is NEVER split into the snapshot fields.
func TestSetFleetCrossRefSplitsVMSnapshot(t *testing.T) {
	threaded := spec.Threaded{
		DeployTraits: map[string]*spec.DeployTraits{
			"vm":  {Venue: "ssh"},
			"pod": {Venue: "container", ImageBacked: true},
		},
	}
	// vm: base:golden → From=base, FromSnapshot=golden
	var vm spec.FleetNode
	SetFleetCrossRef(&vm, "vm", "base:golden", threaded)
	if vm.From != "base" || vm.FromSnapshot != "golden" {
		t.Errorf("vm cross-ref: got From=%q FromSnapshot=%q, want base/golden", vm.From, vm.FromSnapshot)
	}
	// vm: base (no tag) → From=base, FromSnapshot=""
	vm = spec.FleetNode{}
	SetFleetCrossRef(&vm, "vm", "base", threaded)
	if vm.From != "base" || vm.FromSnapshot != "" {
		t.Errorf("vm plain ref: got From=%q FromSnapshot=%q, want base/empty", vm.From, vm.FromSnapshot)
	}
	// pod: ghcr.io/foo/bar:1.2 → Image carries the FULL ref (never split), From/FromSnapshot empty
	var pod spec.FleetNode
	SetFleetCrossRef(&pod, "pod", "ghcr.io/foo/bar:1.2", threaded)
	if pod.Image != "ghcr.io/foo/bar:1.2" || pod.From != "" || pod.FromSnapshot != "" {
		t.Errorf("pod image ref: got Image=%q From=%q FromSnapshot=%q, want the full ref un-split", pod.Image, pod.From, pod.FromSnapshot)
	}
}
