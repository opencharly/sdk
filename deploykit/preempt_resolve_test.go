package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// preempt_resolve_test.go — proves the K1-unblock wave-1 helpers reproduce the former
// charly/preempt.go behavior (gatherPreemptibleHolders/lookupVMClaimant/holderAddrFor/
// gpu-vendor projection) as pure functions of an already-materialized deploy tree, with zero
// LoadUnified dependency.

func TestFilterPreemptibleHolders(t *testing.T) {
	tree := map[string]spec.BundleNode{
		"gpu-workstation": {
			Target:      "vm",
			Descent:     &spec.DescentDescriptor{Venue: "ssh"},
			Preemptible: &spec.PreemptibleConfig{Holds: []string{"nvidia-gpu"}, Restore: "on-success"},
		},
		"plain-pod": {
			Target:  "pod",
			Descent: &spec.DescentDescriptor{Venue: ""},
		},
	}
	got := FilterPreemptibleHolders(tree)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 holder, got %d: %+v", len(got), got)
	}
	h := got[0]
	if h.Name != "gpu-workstation" {
		t.Errorf("Name = %q, want gpu-workstation", h.Name)
	}
	if len(h.Holds) != 1 || h.Holds[0] != "nvidia-gpu" {
		t.Errorf("Holds = %v, want [nvidia-gpu]", h.Holds)
	}
	if h.Restore != "on-success" {
		t.Errorf("Restore = %q, want on-success", h.Restore)
	}
	if h.Addr.Vm != "gpu-workstation" {
		t.Errorf("Addr.Vm = %q, want gpu-workstation (falls back to Base when From is empty)", h.Addr.Vm)
	}
}

func TestFilterPreemptibleHolders_DefaultRestore(t *testing.T) {
	tree := map[string]spec.BundleNode{
		"holder": {Preemptible: &spec.PreemptibleConfig{Holds: []string{"tok"}}},
	}
	got := FilterPreemptibleHolders(tree)
	if len(got) != 1 || got[0].Restore != PreemptRestoreAlways {
		t.Fatalf("expected default restore %q, got %+v", PreemptRestoreAlways, got)
	}
}

func TestFindVMClaimant(t *testing.T) {
	tree := map[string]spec.BundleNode{
		"check-gpu-bed": {
			Target:            "vm",
			From:              "gpu-check-vm",
			Descent:           &spec.DescentDescriptor{Venue: "ssh"},
			RequiresExclusive: []string{"nvidia-gpu"},
		},
		"unrelated": {
			Target:  "pod",
			Descent: &spec.DescentDescriptor{Venue: ""},
		},
	}
	name, node, ok := FindVMClaimant(tree, "gpu-check-vm")
	if !ok {
		t.Fatal("expected a claimant match")
	}
	if name != "check-gpu-bed" {
		t.Errorf("name = %q, want check-gpu-bed", name)
	}
	if node.From != "gpu-check-vm" {
		t.Errorf("node.From = %q, want gpu-check-vm", node.From)
	}

	if _, _, ok := FindVMClaimant(tree, "no-such-entity"); ok {
		t.Error("expected no match for an unreferenced VM entity")
	}
}

func TestFindVMClaimant_RequiresSSHVenue(t *testing.T) {
	// A pod-target node with From+RequiresExclusive set (malformed authoring, or a non-vm
	// substrate reusing From) must NOT match — only an ssh-venue (vm) node is a valid claimant.
	tree := map[string]spec.BundleNode{
		"pod-imposter": {
			Target:            "pod",
			From:              "gpu-check-vm",
			Descent:           &spec.DescentDescriptor{Venue: ""},
			RequiresExclusive: []string{"nvidia-gpu"},
		},
	}
	if _, _, ok := FindVMClaimant(tree, "gpu-check-vm"); ok {
		t.Error("a non-ssh-venue node must never be treated as a VM claimant")
	}
}

func TestHolderAddrFor_DefaultsTargetToPod(t *testing.T) {
	addr := HolderAddrFor("myapp/staging", spec.BundleNode{})
	if addr.Target != "pod" {
		t.Errorf("Target = %q, want pod (empty target defaults to pod)", addr.Target)
	}
	if addr.Base != "myapp" || addr.Instance != "staging" {
		t.Errorf("Base/Instance = %q/%q, want myapp/staging", addr.Base, addr.Instance)
	}
	if addr.Vm != "" {
		t.Errorf("Vm = %q, want empty for a non-ssh-venue node", addr.Vm)
	}
}

func TestGpuVendorTokens(t *testing.T) {
	resources := map[string]*spec.ResolvedResource{
		"nvidia-gpu":       {Gpu: &spec.ResolvedGpuSelector{Vendor: "0x10de"}},
		"arbitration-only": nil,
		"no-gpu-selector":  {},
	}
	got := GpuVendorTokens(resources)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 gpu-backed token, got %d: %+v", len(got), got)
	}
	if got["nvidia-gpu"] != "0x10de" {
		t.Errorf("vendor = %q, want 0x10de", got["nvidia-gpu"])
	}
}

func TestMergedDeployTree_ProjectOnlyWhenNoLocalConfig(t *testing.T) {
	// A nil reader → no per-host overlay merge → the project tree passes through unchanged
	// (#55 coneC-dsh β2+δ: MergedDeployTree now takes a reader; nil is the project-only semantics).
	project := map[string]spec.BundleNode{
		"a": {Target: "pod"},
	}
	merged := MergedDeployTree(project, "test", nil)
	if len(merged) != 1 || merged["a"].Target != "pod" {
		t.Errorf("merged = %+v, want the project tree passed through", merged)
	}
}

// TestMergedDeployTree_PerHostWinsOverCommittedNode proves the preempt arbiter's node view lets a
// PER-HOST preemptible (a local deploy property) override the committed project profile that
// lacks it — so `requires_exclusive` beds can preempt the operator's workstation only where the
// operator opted in, without committing the flag. Relocated from
// charly/deploy_preserve_test.go's TestGatherDeployNodesPerHostWins (#55 final-tail
// split-by-assertion round, team-lead directive 2026-08-03): that test exercised this SAME merge
// claim through two REAL LoadUnified reads (a project dir + a per-host XDG_CONFIG_HOME overlay);
// the merge itself is a pure function of two already-materialized trees, with no loader
// dependency at all, so this is a literal-fixture unit test — the charly-loader half (does
// LoadUnified correctly parse a project's committed `vm: {from: ...}` node and a per-host
// overlay's `preemptible:` override) is already covered generically by charly's own
// node_loader_test.go + the wider VM test suite.
func TestMergedDeployTree_PerHostWinsOverCommittedNode(t *testing.T) {
	project := map[string]spec.BundleNode{
		"cachyos-gpu": {Target: "vm", From: "cachyos-gpu"},
	}
	read := func() (*BundleConfig, error) {
		return &BundleConfig{Bundle: map[string]spec.BundleNode{
			"cachyos-gpu": {Preemptible: &spec.PreemptibleConfig{Holds: []string{"nvidia-gpu"}}},
		}}, nil
	}
	merged := MergedDeployTree(project, "test", read)
	node, ok := merged["cachyos-gpu"]
	if !ok {
		t.Fatal("cachyos-gpu not gathered")
	}
	if node.Preemptible == nil || len(node.Preemptible.Holds) != 1 || node.Preemptible.Holds[0] != "nvidia-gpu" {
		t.Errorf("per-host preemptible did not win over committed project node: got %+v", node.Preemptible)
	}
	if node.From != "cachyos-gpu" { // committed field still present after the merge
		t.Errorf("committed vm field lost in merge: got %q", node.From)
	}
}
