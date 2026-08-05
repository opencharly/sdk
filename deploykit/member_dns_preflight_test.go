package deploykit

// member_dns_preflight_test.go — coverage for the cross-member container-DNS preflight.
//
// The probe boundary (resolveNameFromMember) is stubbed with a fake resolver, the same way the
// bring-up routing tests stub proc.RunCharlySubcommand, so nothing here touches podman or a real
// container.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// hostRefStep builds a container-venue member whose plan addresses each given ${HOST:...} arg.
func hostRefStep(args ...string) *spec.BundleNode {
	n := podNode()
	for _, arg := range args {
		n.Plan = append(n.Plan, spec.Step{
			Check: "reaches the sibling",
			Op:    spec.Op{Command: "curl http://${HOST:" + arg + "}:8080/"},
		})
	}
	return n
}

// TestMemberDNSRefs_OnlyContainerDNSSiblings pins the selection rule: sibling container-DNS refs
// only — not the port-bearing endpoint form, not self-references, not non-members, and not
// members on a non-container venue.
func TestMemberDNSRefs_OnlyContainerDNSSiblings(t *testing.T) {
	node := &spec.BundleNode{Members: map[string]*spec.BundleNode{
		// chrome addresses web (container DNS), api WITH a port (host-vantage endpoint — must be
		// ignored), itself (ignored), and a name that is not a member at all (ignored).
		"chrome": hostRefStep("web", "api:8080", "chrome", "not-a-member"),
		"web":    podNode(),
		"api":    podNode(),
		// A vm member is not on the shared container network; its refs are not this preflight's
		// business even though it declares one.
		"guest": func() *spec.BundleNode {
			n := vmNode("some-vm")
			n.Plan = hostRefStep("web").Plan
			return n
		}(),
	}}

	got := memberDNSRefs(node)
	want := map[string][]string{"chrome": {"web"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("memberDNSRefs = %v, want %v", got, want)
	}
}

// TestMemberDNSRefs_NoRefsIsNil: a group with no cross-member addressing produces no work, which
// is what keeps the preflight free for every bed that does not need it.
func TestMemberDNSRefs_NoRefsIsNil(t *testing.T) {
	node := &spec.BundleNode{Members: map[string]*spec.BundleNode{
		"a": podNode(),
		"b": podNode(),
	}}
	if got := memberDNSRefs(node); len(got) != 0 {
		t.Errorf("expected no DNS refs for a group that does no cross-member addressing, got %v", got)
	}
	if got := memberDNSRefs(nil); got != nil {
		t.Errorf("a nil node has no refs, got %v", got)
	}
}

// TestPreflightMemberDNS_NoRefsSkipsProbing: with nothing to check, the resolver is never called
// (so an unrelated group can never be failed by a host DNS fault it does not depend on).
func TestPreflightMemberDNS_NoRefsSkipsProbing(t *testing.T) {
	orig := resolveNameFromMember
	defer func() { resolveNameFromMember = orig }()
	called := 0
	resolveNameFromMember = func(string, string) error { called++; return nil }

	node := &spec.BundleNode{Members: map[string]*spec.BundleNode{"a": podNode(), "b": podNode()}}
	if err := preflightMemberDNS(node); err != nil {
		t.Fatalf("preflightMemberDNS on a group with no cross-member refs: %v", err)
	}
	if called != 0 {
		t.Errorf("resolver called %d times for a group with no cross-member refs; want 0", called)
	}
}

// TestPreflightMemberDNS_FailureNamesRecovery is the behaviour the RCA asked for: an
// unresolvable sibling fails the BRING-UP (which the runner classes as infra) and the message
// names both the stale-aardvark condition and the recovery command.
func TestPreflightMemberDNS_FailureNamesRecovery(t *testing.T) {
	origResolve := resolveNameFromMember
	origContainer := resolveContainerForPreflight
	defer func() {
		resolveNameFromMember = origResolve
		resolveContainerForPreflight = origContainer
	}()
	resolveContainerForPreflight = func(name, _ string) (string, error) { return "charly-" + name, nil }
	resolveNameFromMember = func(memberKey, name string) error {
		return errors.New("getent: no such host")
	}

	node := &spec.BundleNode{Members: map[string]*spec.BundleNode{
		"chrome": hostRefStep("web"),
		"web":    podNode(),
	}}
	err := preflightMemberDNS(node)
	if err == nil {
		t.Fatal("expected an unresolvable sibling to fail the preflight")
	}
	for _, want := range []string{"chrome", "web", "charly-web", "aardvark-dns", dnsRecoveryCommand} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("preflight error should mention %q; got:\n%s", want, err)
		}
	}
}

// TestPreflightMemberDNS_ResolvableSiblingPasses: the happy path probes exactly the declared pair
// and succeeds.
func TestPreflightMemberDNS_ResolvableSiblingPasses(t *testing.T) {
	origResolve := resolveNameFromMember
	origContainer := resolveContainerForPreflight
	defer func() {
		resolveNameFromMember = origResolve
		resolveContainerForPreflight = origContainer
	}()
	resolveContainerForPreflight = func(name, _ string) (string, error) { return "charly-" + name, nil }
	var probed [][2]string
	resolveNameFromMember = func(memberKey, name string) error {
		probed = append(probed, [2]string{memberKey, name})
		return nil
	}

	node := &spec.BundleNode{Members: map[string]*spec.BundleNode{
		"chrome": hostRefStep("web"),
		"web":    podNode(),
	}}
	if err := preflightMemberDNS(node); err != nil {
		t.Fatalf("preflightMemberDNS with a resolvable sibling: %v", err)
	}
	want := [][2]string{{"chrome", "charly-web"}}
	if !reflect.DeepEqual(probed, want) {
		t.Errorf("probed %v, want %v", probed, want)
	}
}
