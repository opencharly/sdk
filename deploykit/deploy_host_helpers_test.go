package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// testArtifactCandy wraps a name + artifact list into a spec.CandyReader fixture,
// mirroring charly/candy_test_helpers_test.go's testCandy (that file's own helper stays
// charly-side; CandyArtifactRegisters' test needs only the Artifact() accessor, so this
// package keeps its own minimal constructor rather than importing across the boundary).
func testArtifactCandy(name string, artifacts []spec.CandyArtifact) spec.CandyReader {
	return NewSpecCandyModel(spec.CandyModel{Name: name, Artifact: artifacts}, spec.CandyView{Name: name})
}

// TestCandyArtifactRegisters_NameBlind proves the collector reads each candy's OWN
// artifact declaration, never the candy's NAME — the exact axis the P13-KERNEL
// k3s-server dispatch fix targets. A candy literally named "k3s-server" contributes
// NOTHING unless it actually declares a `register:` hint on an artifact; a candy with an
// entirely different name contributes "kubeconfig" when it does declare one.
func TestCandyArtifactRegisters_NameBlind(t *testing.T) {
	unhinted := testArtifactCandy("k3s-server", []spec.CandyArtifact{
		{Name: "kubeconfig", Path: "/etc/rancher/k3s/k3s.yaml", RetrieveTo: "/tmp/x"},
	})
	hinted := testArtifactCandy("totally-different-name", []spec.CandyArtifact{
		{Name: "kubeconfig", Path: "/etc/rancher/k3s/k3s.yaml", RetrieveTo: "/tmp/x", Register: "kubeconfig"},
	})
	other := testArtifactCandy("other-candy", []spec.CandyArtifact{
		{Name: "state", Path: "/var/lib/other/state.json", RetrieveTo: "/tmp/y"},
	})

	t.Run("name k3s-server alone triggers nothing", func(t *testing.T) {
		got := CandyArtifactRegisters([]spec.CandyReader{unhinted})
		if len(got) != 0 {
			t.Fatalf("expected no register hints for a candy with no register: declaration, got %v", got)
		}
	})

	t.Run("declared register: kubeconfig triggers regardless of candy name", func(t *testing.T) {
		got := CandyArtifactRegisters([]spec.CandyReader{hinted, other})
		if !got["kubeconfig"] || len(got) != 1 {
			t.Fatalf("expected exactly {kubeconfig: true}, got %v", got)
		}
	})

	t.Run("nil/empty layers -> empty set", func(t *testing.T) {
		if got := CandyArtifactRegisters(nil); len(got) != 0 {
			t.Fatalf("expected empty set for nil layers, got %v", got)
		}
	})
}

// TestHostReverseExec_AccessorPassthrough covers the ReverseExecutor adapter the
// host-venue teardown (externalDeployTarget.Del → TeardownHostDeploy) hands to
// kit.RunReverseOps. Relocated from charly/deploy_host_helpers_test.go when
// HostReverseExec/TeardownHostDeploy moved to sdk/deploykit (P13-KERNEL, the 4/5 sdk
// lift); their end-to-end teardown is exercised live by the check-local bed's
// `charly bundle del`.
func TestHostReverseExec_AccessorPassthrough(t *testing.T) {
	e := &HostReverseExec{
		DryRun:          true,
		KeepRepoChanges: true,
		KeepServices:    false,
		Runner:          nil,
	}
	if !e.ReverseDryRun() {
		t.Errorf("ReverseDryRun = false, want true")
	}
	if !e.ReverseKeepRepoChanges() {
		t.Errorf("ReverseKeepRepoChanges = false, want true")
	}
	if e.ReverseKeepServices() {
		t.Errorf("ReverseKeepServices = true, want false")
	}
	if e.ReverseRunner() != nil {
		t.Errorf("ReverseRunner non-nil, want nil")
	}
}
