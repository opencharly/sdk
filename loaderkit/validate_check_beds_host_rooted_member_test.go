package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// sdk#330 — the load-time control on the SILENT host deploy: a HOST-ROOTED deploy-level bed member
// with no `host:` is walked as its own root and lands on the machine running charly, not in the
// bed's venue. The guard refuses it at resolve, naming the bed + member and both legal remedies.
//
// Host-rootedness is the STAMPED descent trait (spec/deploy.HostRooted), so the fixtures set
// Descent directly — the same shape spec/deploy's own deploy_ops_member_test.go uses. The
// predicate's trait source is proven there (spec/descent.go); this test pins the GUARD.

// hostRooted / containerVenue / sshVenue are the three stamped descents the guard distinguishes.
func hostRootedDescent() *spec.DescentDescriptor {
	return &spec.DescentDescriptor{Transport: "none", Venue: "shell", HostRooted: true}
}

func containerDescent() *spec.DescentDescriptor {
	return &spec.DescentDescriptor{Transport: "container-exec", Venue: "container", ImageBacked: true}
}

// TestValidateBedMemberPositions_RefusesUnmarkedHostRootedMember pins the ONE refusal: a
// deploy-level (alongside) host-rooted member with an empty `host:`.
func TestValidateBedMemberPositions_RefusesUnmarkedHostRootedMember(t *testing.T) {
	bed := spec.DeployNode{
		Target: "pod",
		Member: []spec.Member{{
			Name:     "host-driver",
			Position: spec.PositionDeployLevel,
			Node:     &spec.DeployNode{Target: "local", Descent: hostRootedDescent()},
		}},
	}
	err := validateBedMemberPositions("check-cross-local-http", &bed)
	if err == nil {
		t.Fatalf("unmarked host-rooted deploy-level member accepted, want refusal")
	}
	// The message must be actionable: it names the bed, the member, and BOTH remedies.
	for _, want := range []string{"check-cross-local-http", "host-driver", "host: local", "Nest it inside"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal does not name %q: %v", want, err)
		}
	}
}

// TestValidateBedMemberPositions_AcceptsTheThreeLegalShapes pins that the guard refuses ONLY the
// unmarked host-rooted deploy-level member: the explicit marking, the in-substrate position, and a
// non-host-rooted (container) deploy-level sibling all pass.
func TestValidateBedMemberPositions_AcceptsTheThreeLegalShapes(t *testing.T) {
	marked := spec.DeployNode{
		Member: []spec.Member{{
			Name:     "host-driver",
			Position: spec.PositionDeployLevel,
			Node:     &spec.DeployNode{Target: "local", Host: "local", Descent: hostRootedDescent()},
		}},
	}
	if err := validateBedMemberPositions("bed-marked", &marked); err != nil {
		t.Fatalf("explicitly marked `host: local` member rejected: %v", err)
	}

	// The #763 shape: a host-rooted member authored INSIDE the kind body deploys into the bed's
	// venue (plugin-deploy-vm's nested local path) — never refused.
	nested := spec.DeployNode{
		Member: []spec.Member{{
			Name:     "arch-host",
			Position: spec.PositionInSubstrate,
			Node:     &spec.DeployNode{Target: "local", Descent: hostRootedDescent()},
		}},
	}
	if err := validateBedMemberPositions("bed-nested", &nested); err != nil {
		t.Fatalf("in-substrate host-rooted member rejected: %v", err)
	}

	// A container sibling alongside its bed is a separate pod, not a host deploy: the predicate,
	// not the position alone, is what the guard reads.
	podSibling := spec.DeployNode{
		Member: []spec.Member{{
			Name:     "peer-pod",
			Position: spec.PositionDeployLevel,
			Node:     &spec.DeployNode{Target: "pod", Descent: containerDescent()},
		}},
	}
	if err := validateBedMemberPositions("bed-peer", &podSibling); err != nil {
		t.Fatalf("non-host-rooted deploy-level member rejected: %v", err)
	}
}

// TestValidateCheckBeds_HostRootedMemberHooksIn pins the validator's WIRING: the guard is reached
// through the real load-time entrypoint every command that resolves a bed runs through — and the
// NEGATIVE CONTROL in the same fixture (the identical bed with `host: local`) proves it is the
// guard, not an unrelated rule, that rejects. podTargetThreaded supplies the bed_target trait so
// the bed-target arm runs and control reaches the member guard.
func TestValidateCheckBeds_HostRootedMemberHooksIn(t *testing.T) {
	threaded := spec.Threaded{DeployTraits: map[string]*spec.DeployTraits{
		"pod": {Venue: "container", BedTarget: true, ImageBacked: true},
	}}
	disp := true
	bedWith := func(host string) *spec.UnifiedFile {
		return &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{
			"check-cross-local-http": {
				Target:     "pod",
				Disposable: &disp,
				Member: []spec.Member{{
					Name:     "host-driver",
					Position: spec.PositionDeployLevel,
					Node:     &spec.DeployNode{Target: "local", Host: host, Descent: hostRootedDescent()},
				}},
			},
		}}
	}

	// Negative control FIRST: with the explicit marking the same bed resolves clean, so the
	// rejection below cannot be some unrelated rule firing.
	if err := ValidateCheckBeds(bedWith("local"), threaded); err != nil {
		t.Fatalf("marked bed rejected by ValidateCheckBeds (control failed): %v", err)
	}

	err := ValidateCheckBeds(bedWith(""), threaded)
	if err == nil {
		t.Fatal("unmarked host-rooted deploy-level member passed ValidateCheckBeds, want load-time rejection")
	}
	if !strings.Contains(err.Error(), "host-driver") {
		t.Fatalf("load-time rejection does not name the member: %v", err)
	}
}

// TestValidateBedMemberPositions_WalksTheWholeMemberTree pins the recursion: the same defect one
// level down (a host-rooted deploy-level child of a member) is named by its dotted address, so the
// marking sweep can enumerate it instead of it staying silent.
func TestValidateBedMemberPositions_WalksTheWholeMemberTree(t *testing.T) {
	deep := spec.DeployNode{
		Member: []spec.Member{{
			Name:     "outer",
			Position: spec.PositionInSubstrate,
			Node: &spec.DeployNode{
				Target: "pod",
				Member: []spec.Member{{
					Name:     "inner-driver",
					Position: spec.PositionDeployLevel,
					Node:     &spec.DeployNode{Target: "local", Descent: hostRootedDescent()},
				}},
			},
		}},
	}
	err := validateBedMemberPositions("bed-deep", &deep)
	if err == nil {
		t.Fatalf("unmarked host-rooted member one level down accepted, want refusal")
	}
	if !strings.Contains(err.Error(), "outer.inner-driver") {
		t.Fatalf("refusal does not name the dotted member address: %v", err)
	}
}
