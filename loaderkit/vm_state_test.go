package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// vm_state_test.go — the persisted-VmDeployState extraction (K-wave 2 cone R2 bank D: the
// config-resolve HostBuild seam's VmState leg moved here as VmStateFromBundleConfig +
// ResolveVmStateViaExecutor). The pure lookup is unit-tested directly; the executor-backed
// ResolveVmStateViaExecutor is exercised by the plugin callers' stub seams + the live VM beds.

func TestVmStateFromBundleConfig(t *testing.T) {
	// A present entry yields its VmState.
	dc := &spec.BundleConfig{Bundle: map[string]spec.BundleNode{
		"vm:arch": {VmState: &spec.VmDeployState{SshPort: 2244}},
	}}
	if got := VmStateFromBundleConfig(dc, "arch"); got == nil || got.SshPort != 2244 {
		t.Fatalf("VmStateFromBundleConfig(arch) = %+v, want SshPort=2244", got)
	}
	// A missing entity degrades to nil.
	if got := VmStateFromBundleConfig(dc, "missing"); got != nil {
		t.Fatalf("VmStateFromBundleConfig(missing) = %+v, want nil", got)
	}
	// A nil BundleConfig (unreadable overlay) degrades to nil, not a panic.
	if got := VmStateFromBundleConfig(nil, "arch"); got != nil {
		t.Fatalf("VmStateFromBundleConfig(nil) = %+v, want nil", got)
	}
}
