package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// vm_state_test.go — the persisted-VmDeployState extraction (K-wave 2 cone R2 bank D: the
// config-resolve HostBuild seam's VmState leg moved here as VmStateFromFleetConfig +
// ResolveVmStateViaExecutor). The pure lookup is unit-tested directly; the executor-backed
// ResolveVmStateViaExecutor is exercised by the plugin callers' stub seams + the live VM beds.

func TestVmStateFromFleetConfig(t *testing.T) {
	// A present entry yields its VmState.
	dc := &spec.FleetConfig{Fleet: map[string]spec.FleetNode{
		"vm:arch": {VmState: &spec.VmDeployState{SshPort: 2244}},
	}}
	if got := VmStateFromFleetConfig(dc, "arch"); got == nil || got.SshPort != 2244 {
		t.Fatalf("VmStateFromFleetConfig(arch) = %+v, want SshPort=2244", got)
	}
	// A missing entity degrades to nil.
	if got := VmStateFromFleetConfig(dc, "missing"); got != nil {
		t.Fatalf("VmStateFromFleetConfig(missing) = %+v, want nil", got)
	}
	// A nil FleetConfig (unreadable overlay) degrades to nil, not a panic.
	if got := VmStateFromFleetConfig(nil, "arch"); got != nil {
		t.Fatalf("VmStateFromFleetConfig(nil) = %+v, want nil", got)
	}
}
