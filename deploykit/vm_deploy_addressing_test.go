package deploykit

import (
	"path/filepath"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// TestResolveVmSshPort covers the three resolution paths: the 2222 default, an explicit fixed
// port, and ssh.port_auto auto-allocation (no persisted state → a fresh ephemeral host port).
// Relocated from charly/vm_ssh_port_test.go (FLOOR-SLIM Unit 3) — ResolveVmSshPort is now a
// deploykit function. The overlay path is redirected to a tempdir so the port_auto branch's
// LoadDeployConfigForRead call never reads (or races) a real ~/.config/charly/charly.yml.
func TestResolveVmSshPort(t *testing.T) {
	t.Setenv(kit.DeployConfigEnv, filepath.Join(t.TempDir(), "charly.yml"))

	// Default: no SSH block → 2222.
	if p, err := ResolveVmSshPort(&spec.ResolvedVm{}, "vm-ssh-port-default-zzz"); err != nil || p != 2222 {
		t.Fatalf("default: got (%d, %v), want (2222, nil)", p, err)
	}
	// Explicit fixed port.
	if p, err := ResolveVmSshPort(&spec.ResolvedVm{SSH: &spec.VmSSH{Port: 2244}}, "vm-ssh-port-fixed-zzz"); err != nil || p != 2244 {
		t.Fatalf("fixed: got (%d, %v), want (2244, nil)", p, err)
	}
	// port_auto with a VM name absent from the (redirected, empty) overlay → allocate a free
	// port. (The ephemeral range is high, so it is never the 2222 default — a default here would
	// mean the port_auto branch silently did nothing.)
	p, err := ResolveVmSshPort(&spec.ResolvedVm{SSH: &spec.VmSSH{PortAuto: true}}, "vm-ssh-port-auto-nonexistent-zzz")
	if err != nil {
		t.Fatalf("port_auto: unexpected error: %v", err)
	}
	if p <= 0 || p > 65535 {
		t.Fatalf("port_auto: allocated port %d out of range 1-65535", p)
	}
	if p == 2222 {
		t.Errorf("port_auto: got the 2222 default instead of an allocated ephemeral port")
	}
}

// vm_deploy_addressing_test.go — sdk-level coverage for the VM deploy-state helpers relocated out
// of charly/vm_deploy_state.go by FLOOR-SLIM Unit 3 (a pure *BundleConfig-shaped mechanism, not
// LoadUnified-coupled).

// TestPruneStaleVmDottedTwin is the regression test for a nested (dotted) deploy's per-host
// vm_state that used to get written TWICE — once correctly under "vm:"+VmDomainIdentity(name),
// once under the RAW dotted name — poisoning the overlay on every subsequent load.
// PruneStaleVmDottedTwin is the pure self-healing scan the charly-side write path runs on every
// write.
func TestPruneStaleVmDottedTwin(t *testing.T) {
	t.Run("removes a matching dotted twin", func(t *testing.T) {
		dc := &BundleConfig{Bundle: map[string]BundleNode{
			"check-sidecar-pod.check-sidecar-pod-ephvm":    {Target: "vm", VmState: &spec.VmDeployState{SshPort: 45551}},
			"vm:check-sidecar-pod-check-sidecar-pod-ephvm": {Target: "vm", VmState: &spec.VmDeployState{SshPort: 33799}},
		}}
		got := PruneStaleVmDottedTwin(dc, "vm:check-sidecar-pod-check-sidecar-pod-ephvm")
		if got != "check-sidecar-pod.check-sidecar-pod-ephvm" {
			t.Errorf("PruneStaleVmDottedTwin() = %q, want the dotted twin's key", got)
		}
		if _, stillThere := dc.Bundle["check-sidecar-pod.check-sidecar-pod-ephvm"]; stillThere {
			t.Error("dotted twin was not removed from dc.Bundle")
		}
		if _, canonical := dc.Bundle["vm:check-sidecar-pod-check-sidecar-pod-ephvm"]; !canonical {
			t.Error("the canonical entry itself was wrongly removed")
		}
	})
	t.Run("no twin present is a no-op", func(t *testing.T) {
		dc := &BundleConfig{Bundle: map[string]BundleNode{
			"vm:myapp": {Target: "vm"},
		}}
		if got := PruneStaleVmDottedTwin(dc, "vm:myapp"); got != "" {
			t.Errorf("PruneStaleVmDottedTwin() = %q, want \"\" (nothing to prune)", got)
		}
	})
	t.Run("does not over-match an unrelated dotted entry", func(t *testing.T) {
		dc := &BundleConfig{Bundle: map[string]BundleNode{
			"vm:myapp":                 {Target: "vm"},
			"other-stack.other-member": {Target: "vm"}, // a DIFFERENT domain's dotted entry
		}}
		if got := PruneStaleVmDottedTwin(dc, "vm:myapp"); got != "" {
			t.Errorf("PruneStaleVmDottedTwin() = %q, want \"\" (unrelated domain must survive)", got)
		}
		if _, survived := dc.Bundle["other-stack.other-member"]; !survived {
			t.Error("unrelated dotted entry was wrongly pruned (over-match)")
		}
	})
	t.Run("the canonical key itself is never pruned even though it is its own domain match", func(t *testing.T) {
		dc := &BundleConfig{Bundle: map[string]BundleNode{
			"vm:myapp": {Target: "vm"},
		}}
		PruneStaleVmDottedTwin(dc, "vm:myapp")
		if _, ok := dc.Bundle["vm:myapp"]; !ok {
			t.Error("the canonical entry was wrongly self-pruned")
		}
	})
}

// TestVmDeployEntryKeys exercises the `vm:`-form From-scan: a kind:check VM bed (e.g.
// check-k3s-vm) writes its vm_state under the BUNDLE key (check-k3s-vm) cross-referencing the VM
// ENTITY (k3s-vm). The scan lets the DIRECT `charly vm destroy k3s-vm` path (which builds
// "vm:k3s-vm") still resolve the bundle-keyed entry via that cross-ref. The scan must not
// over-match an UNRELATED bundle.
func TestVmDeployEntryKeys(t *testing.T) {
	dc := &BundleConfig{Bundle: map[string]BundleNode{
		"check-k3s-vm":   {Target: "vm", From: "k3s-vm"},
		"check-other-vm": {Target: "vm", From: "other-vm"},
	}}

	t.Run("vm:-prefixed entity resolves the bundle-keyed entry via the From cross-ref", func(t *testing.T) {
		keys := VmDeployEntryKeys(dc, "vm:k3s-vm")
		if len(keys) != 1 || keys[0] != "check-k3s-vm" {
			t.Errorf("VmDeployEntryKeys(vm:k3s-vm) = %v, want [check-k3s-vm]", keys)
		}
	})

	t.Run("plain deploy name takes only the literal-key path (no scan, no over-match)", func(t *testing.T) {
		keys := VmDeployEntryKeys(dc, "check-k3s-vm")
		if len(keys) != 1 || keys[0] != "check-k3s-vm" {
			t.Errorf("VmDeployEntryKeys(check-k3s-vm) = %v, want [check-k3s-vm]", keys)
		}
	})

	t.Run("unknown key resolves to nothing", func(t *testing.T) {
		if keys := VmDeployEntryKeys(dc, "vm:nonexistent"); len(keys) != 0 {
			t.Errorf("VmDeployEntryKeys(vm:nonexistent) = %v, want empty", keys)
		}
	})
}
