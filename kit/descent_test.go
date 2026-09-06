package kit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// canonicalTraits mirrors the substrate plugin's DECLARED per-word #DeployTraits (the
// Appendix-B canonical table). The real traitsFor resolves these via the provider
// registry (deployTraitsFor); the kit tests inject them directly to exercise the
// GENERIC traits→transport derivation without a registry.
func canonicalTraits(word string) *spec.DeployTraits {
	switch word {
	case "pod":
		return &spec.DeployTraits{Venue: "container", ImageBacked: true, ImageContext: true}
	case "vm":
		return &spec.DeployTraits{Venue: "ssh", MachineVenue: true, ExclusiveVenue: true}
	case "local":
		return &spec.DeployTraits{Venue: "shell", MachineVenue: true}
	case "kubernetes":
		return &spec.DeployTraits{Venue: "shell", ImageContext: true, LeafOnly: true}
	case "android":
		return &spec.DeployTraits{Venue: "parent"}
	default:
		return nil // targetless group / empty target
	}
}

func TestDescentFromTraits_TransportsByTraits(t *testing.T) {
	cases := []struct {
		target        string
		wantTransport string
		wantHostRoot  bool
	}{
		{"pod", "container-exec", false},
		{"vm", "ssh", false},
		{"kubernetes", "reject", false},
		{"local", "none", true},
		{"android", "none", false},
		{"", "container-exec", false}, // targetless group → external-in-place default
	}
	for _, c := range cases {
		d := DescentFromTraits(canonicalTraits(c.target))
		if d == nil {
			t.Fatalf("target %q: nil descent", c.target)
		}
		if d.Transport != c.wantTransport {
			t.Errorf("target %q: transport = %q, want %q", c.target, d.Transport, c.wantTransport)
		}
		if d.HostRooted != c.wantHostRoot {
			t.Errorf("target %q: host_rooted = %v, want %v", c.target, d.HostRooted, c.wantHostRoot)
		}
	}
}

func TestDescentFromTraits_CopiesDeclaredTraits(t *testing.T) {
	d := DescentFromTraits(canonicalTraits("pod"))
	if d.Venue != "container" || !d.ImageBacked || !d.ImageContext {
		t.Errorf("pod traits not copied onto descent: %+v", d)
	}
	if v := DescentFromTraits(canonicalTraits("vm")); v.Venue != "ssh" || !v.MachineVenue || !v.ExclusiveVenue {
		t.Errorf("vm traits not copied onto descent: %+v", v)
	}
	if k := DescentFromTraits(canonicalTraits("kubernetes")); !k.LeafOnly || k.Venue != "shell" {
		t.Errorf("kubernetes traits not copied onto descent: %+v", k)
	}
}

func TestStampDescent_RecursesNestedAndPeer(t *testing.T) {
	// The ONE ordered member tree (Cutover C task 0): an in-substrate member and a
	// deploy-level member both recurse from the root's single Member list.
	root := &spec.Deploy{
		Target: "vm",
		Member: []spec.Member{
			{Name: "inner", Position: spec.PositionInSubstrate, Node: &spec.Deploy{Target: "pod"}},
			{Name: "peer", Position: spec.PositionDeployLevel, Node: &spec.Deploy{Target: "local"}},
		},
	}
	StampDescent(root, canonicalTraits)
	if root.Descent == nil || root.Descent.Transport != "ssh" {
		t.Fatalf("root descent = %+v, want ssh", root.Descent)
	}
	if c := root.MemberByName("inner").Node; c.Descent == nil || c.Descent.Transport != "container-exec" {
		t.Errorf("nested member descent = %+v, want container-exec", c.Descent)
	}
	if m := root.MemberByName("peer").Node; m.Descent == nil || !m.Descent.HostRooted {
		t.Errorf("deploy-level member descent = %+v, want host_rooted", m.Descent)
	}
}
