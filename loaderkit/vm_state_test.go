package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// vm_state_test.go — the persisted-VmDeployState extraction (K-wave 2 cone R2 bank D: the
// config-resolve HostBuild seam's VmState leg moved here as VmStateFromDeployConfig +
// ResolveVmStateViaExecutor). The pure lookup is unit-tested directly; the executor-backed
// ResolveVmStateViaExecutor is exercised by the plugin callers' stub seams + the live VM beds.

func TestVmStateFromDeployConfig(t *testing.T) {
	// A present entry yields its VmState.
	dc := &spec.DeployConfig{Deploy: map[string]spec.DeployNode{
		"vm:arch": {VmState: &spec.VmDeployState{SSHPort: 2244}},
	}}
	if got := VmStateFromDeployConfig(dc, "arch"); got == nil || got.SSHPort != 2244 {
		t.Fatalf("VmStateFromDeployConfig(arch) = %+v, want SSHPort=2244", got)
	}
	// A missing entity degrades to nil.
	if got := VmStateFromDeployConfig(dc, "missing"); got != nil {
		t.Fatalf("VmStateFromDeployConfig(missing) = %+v, want nil", got)
	}
	// A nil DeployConfig (unreadable overlay) degrades to nil, not a panic.
	if got := VmStateFromDeployConfig(nil, "arch"); got != nil {
		t.Fatalf("VmStateFromDeployConfig(nil) = %+v, want nil", got)
	}
}
