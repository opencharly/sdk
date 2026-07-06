package kit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestStampDescent_TransportsByTarget(t *testing.T) {
	cases := []struct {
		target        string
		wantTransport string
		wantHostRoot  bool
	}{
		{"pod", "container-exec", false},
		{"vm", "ssh", false},
		{"k8s", "reject", false},
		{"local", "none", true},
		{"android", "none", false},
		{"", "container-exec", false}, // targetless group → pod default
	}
	for _, c := range cases {
		n := &spec.Deploy{Target: c.target}
		StampDescent(n)
		if n.Descent == nil {
			t.Fatalf("target %q: nil descent", c.target)
		}
		if n.Descent.Transport != c.wantTransport {
			t.Errorf("target %q: transport = %q, want %q", c.target, n.Descent.Transport, c.wantTransport)
		}
		if n.Descent.HostRooted != c.wantHostRoot {
			t.Errorf("target %q: host_rooted = %v, want %v", c.target, n.Descent.HostRooted, c.wantHostRoot)
		}
	}
}

func TestStampDescent_RecursesNestedAndPeer(t *testing.T) {
	root := &spec.Deploy{
		Target: "vm",
		Children: map[string]*spec.Deploy{
			"inner": {Target: "pod"},
		},
		Members: map[string]*spec.Deploy{
			"peer": {Target: "local"},
		},
	}
	StampDescent(root)
	if root.Descent == nil || root.Descent.Transport != "ssh" {
		t.Fatalf("root descent = %+v, want ssh", root.Descent)
	}
	if c := root.Children["inner"]; c.Descent == nil || c.Descent.Transport != "container-exec" {
		t.Errorf("nested child descent = %+v, want container-exec", c.Descent)
	}
	if m := root.Members["peer"]; m.Descent == nil || !m.Descent.HostRooted {
		t.Errorf("peer member descent = %+v, want host_rooted", m.Descent)
	}
}
