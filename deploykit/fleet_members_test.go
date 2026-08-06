package deploykit

// fleet_members_test.go — relocated + adapted from charly/fleet_members_test.go (#55 W3 A4).
// TestIsPodMember/TestTearDownMembers_* covered isPodMember/tearDownMembers via bare
// spec.FleetNode{Target: "pod"} fixtures, relying on core's OLD nodeTraits' registry-fallback
// branch (Descent nil → resolve via the live provider registry). The relocated
// spec.IsVmVenue/spec.IsContainerVenue predicates read ONLY the wire-stamped node.Descent (no
// registry access, matching candy/plugin-check's own registry-free twin) — every node
// BringUpMembers/TearDownMembers sees in practice comes from an already-loaded, Descent-stamped
// project, so fixtures here stamp Descent directly instead of relying on a registry fallback that
// no longer exists at this layer.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/spec"
)

func vmNode(from string) *spec.FleetNode {
	return &spec.FleetNode{From: from, Descent: &spec.DescentDescriptor{Venue: "ssh"}}
}

func podNode() *spec.FleetNode {
	return &spec.FleetNode{Descent: &spec.DescentDescriptor{Venue: "container"}}
}

func localNode() *spec.FleetNode {
	return &spec.FleetNode{Descent: &spec.DescentDescriptor{Venue: "shell", HostRooted: true}}
}

// TestIsVmVenue_IsContainerVenue covers the routing predicates BringUpMembers/TearDownMembers
// dispatch on, over Descent-stamped fixtures (the shape every LoadUnified'd node carries).
func TestIsVmVenue_IsContainerVenue(t *testing.T) {
	if !spec.IsContainerVenue(podNode()) {
		t.Errorf("a container-venue node should be a pod member")
	}
	if spec.IsContainerVenue(vmNode("x")) || spec.IsContainerVenue(localNode()) {
		t.Errorf("vm/local venue nodes should NOT be container members")
	}
	if !spec.IsVmVenue(vmNode("x")) {
		t.Errorf("an ssh-venue node should be a vm member")
	}
	if spec.IsVmVenue(podNode()) || spec.IsVmVenue(localNode()) {
		t.Errorf("container/local venue nodes should NOT be vm members")
	}
	if spec.IsContainerVenue(nil) || spec.IsVmVenue(nil) {
		t.Errorf("a nil node is neither venue")
	}
}

// TestTearDownMembers_RoutingAndOrder: TearDownMembers iterates members in sorted order and
// routes a pod member to `charly remove --purge`, a non-pod member to `charly fleet del
// --assume-yes` — the same iteration/routing logic BringUpMembers uses, verified here with the
// stubbable proc.RunCharlySubcommand package var (no side effects). The flag itself is proven
// valid against real Kong parsing by TestFleetDelArgv_KongAccepts (this stub-based test cannot —
// it never invokes flag parsing, which is exactly how a `--yes`/`--force` drift once slipped
// through).
func TestTearDownMembers_RoutingAndOrder(t *testing.T) {
	orig := proc.RunCharlySubcommand
	defer func() { proc.RunCharlySubcommand = orig }()
	var calls [][]string
	proc.RunCharlySubcommand = func(args ...string) error {
		calls = append(calls, args)
		return nil
	}
	node := &spec.FleetNode{Members: map[string]*spec.FleetNode{
		"zeta-pod":   podNode(),
		"alpha-host": localNode(),
	}}
	if err := TearDownMembers(node); err != nil {
		t.Fatalf("TearDownMembers: %v", err)
	}
	want := [][]string{
		spec.FleetDelArgv("alpha-host"),  // sorted first; non-pod → deploy del --assume-yes (unattended)
		{"remove", "zeta-pod", "--purge"}, // pod → remove --purge
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("TearDownMembers calls = %v, want %v", calls, want)
	}
}

// TestTearDownMembers_NoMembersNoop: nothing happens when there are no members.
func TestTearDownMembers_NoMembersNoop(t *testing.T) {
	orig := proc.RunCharlySubcommand
	defer func() { proc.RunCharlySubcommand = orig }()
	called := false
	proc.RunCharlySubcommand = func(args ...string) error { called = true; return nil }
	if err := TearDownMembers(&spec.FleetNode{}); err != nil {
		t.Fatalf("TearDownMembers(empty): %v", err)
	}
	if called {
		t.Errorf("TearDownMembers ran a subcommand for a node with no members")
	}
}

func TestTearDownMembers_AttemptsAllAndReturnsJoinedErrors(t *testing.T) {
	orig := proc.RunCharlySubcommand
	defer func() { proc.RunCharlySubcommand = orig }()
	firstErr := errors.New("first teardown failed")
	secondErr := errors.New("second teardown failed")
	var calls [][]string
	proc.RunCharlySubcommand = func(args ...string) error {
		calls = append(calls, args)
		if len(calls) == 1 {
			return firstErr
		}
		return secondErr
	}
	err := TearDownMembers(&spec.FleetNode{Members: map[string]*spec.FleetNode{
		"a-local": localNode(),
		"b-pod":   podNode(),
	}})
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("TearDownMembers error = %v, want both member failures", err)
	}
	if len(calls) != 2 {
		t.Fatalf("TearDownMembers stopped early: calls = %v", calls)
	}
}

// TestBringUpMembers_NoMembersNoop mirrors the teardown no-op guard on the bring-up side.
func TestBringUpMembers_NoMembersNoop(t *testing.T) {
	orig := proc.RunCharlySubcommand
	defer func() { proc.RunCharlySubcommand = orig }()
	called := false
	proc.RunCharlySubcommand = func(args ...string) error { called = true; return nil }
	if err := BringUpMembers(&spec.FleetNode{}, ""); err != nil {
		t.Fatalf("BringUpMembers(empty): %v", err)
	}
	if called {
		t.Errorf("BringUpMembers ran a subcommand for a node with no members")
	}
}

// TestWithMemberTag covers the --tag appender BringUpMembers threads through every step.
func TestWithMemberTag(t *testing.T) {
	if got := withMemberTag([]string{"config", "x"}, ""); !reflect.DeepEqual(got, []string{"config", "x"}) {
		t.Errorf("withMemberTag empty tag = %v, want unchanged", got)
	}
	if got := withMemberTag([]string{"config", "x"}, "v1"); !reflect.DeepEqual(got, []string{"config", "x", "--tag", "v1"}) {
		t.Errorf("withMemberTag = %v, want --tag appended", got)
	}
}
