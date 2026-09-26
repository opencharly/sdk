package deploykit

import (
	"path/filepath"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
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
	if p, err := ResolveVmSshPort(&spec.ResolvedVm{SSH: &spec.VmSsh{Port: 2244}}, "vm-ssh-port-fixed-zzz"); err != nil || p != 2244 {
		t.Fatalf("fixed: got (%d, %v), want (2244, nil)", p, err)
	}
	// port_auto with a VM name absent from the (redirected, empty) overlay → allocate a free
	// port. (The ephemeral range is high, so it is never the 2222 default — a default here would
	// mean the port_auto branch silently did nothing.)
	p, err := ResolveVmSshPort(&spec.ResolvedVm{SSH: &spec.VmSsh{PortAuto: true}}, "vm-ssh-port-auto-nonexistent-zzz")
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
// of charly/vm_deploy_state.go by FLOOR-SLIM Unit 3 (a pure *DeployConfig-shaped mechanism, not
// LoadUnified-coupled).

// TestVmDeployEntryKeys exercises the entity From-scan: a kind:check VM bed (e.g. check-k3s-vm)
// writes its vm_state under the DEPLOY IDENTITY (check-k3s-vm) cross-referencing the VM ENTITY
// (k3s-vm). The scan lets the DIRECT `charly vm destroy k3s-vm` path (which passes the ENTITY, not
// a deploy identity) still resolve the deploy-keyed entry via that cross-ref. The scan must not
// over-match an UNRELATED deploy.
func TestVmDeployEntryKeys(t *testing.T) {
	dc := &DeployConfig{Deploy: map[string]DeployNode{
		"check-k3s-vm":   {Target: "vm", From: "k3s-vm"},
		"check-other-vm": {Target: "vm", From: "other-vm"},
	}}

	t.Run("entity argument resolves the deploy-keyed entry via the From cross-ref", func(t *testing.T) {
		keys := VmDeployEntryKeys(dc, "k3s-vm")
		if len(keys) != 1 || keys[0] != "check-k3s-vm" {
			t.Errorf("VmDeployEntryKeys(k3s-vm) = %v, want [check-k3s-vm]", keys)
		}
	})

	t.Run("deploy identity takes the literal-key path", func(t *testing.T) {
		keys := VmDeployEntryKeys(dc, "check-k3s-vm")
		if len(keys) != 1 || keys[0] != "check-k3s-vm" {
			t.Errorf("VmDeployEntryKeys(check-k3s-vm) = %v, want [check-k3s-vm]", keys)
		}
	})

	t.Run("a namespaced identity key is literal", func(t *testing.T) {
		nsdc := &DeployConfig{Deploy: map[string]DeployNode{
			"charly.check-k3s-vm": {Target: "vm", From: "k3s-vm"},
		}}
		keys := VmDeployEntryKeys(nsdc, "charly.check-k3s-vm")
		if len(keys) != 1 || keys[0] != "charly.check-k3s-vm" {
			t.Errorf("VmDeployEntryKeys(charly.check-k3s-vm) = %v, want [charly.check-k3s-vm]", keys)
		}
	})

	t.Run("unknown key resolves to nothing", func(t *testing.T) {
		if keys := VmDeployEntryKeys(dc, "nonexistent"); len(keys) != 0 {
			t.Errorf("VmDeployEntryKeys(nonexistent) = %v, want empty", keys)
		}
	})
}
