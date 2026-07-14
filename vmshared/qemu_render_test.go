package vmshared

import (
	"strings"
	"testing"
)

// TestRenderQemuNic_ForwardsFromResolvedRtOnly proves the extra-port-forward
// cutover: the qemu renderer emits forwards ONLY from the RESOLVED
// rt.ExtraPortForwards (the host-side-allocated strings), never from
// spec.Network.PortForwards directly — the same spec=intent / rt=resolved rule
// the SSH port already follows. This test FAILS on the pre-feature renderer
// (which looped spec.Network.PortForwards and would emit 16443 below).
func TestRenderQemuNic_ForwardsFromResolvedRtOnly(t *testing.T) {
	spec := &VmSpec{
		// Authored intent carries the sentinel form; the renderer must NOT read it.
		Network: &VmNetwork{Mode: "user", PortForwards: []string{"auto:6443", "16443:6443"}},
	}
	rt := VmRuntimeParams{
		SshPort: 2299,
		// The orchestrator resolved auto:6443 → an allocated host port and passes
		// the concrete strings here. This is the ONLY forward source the renderer reads.
		ExtraPortForwards: []string{"45000:6443"},
	}
	args := strings.Join(renderQemuNic(spec, rt), " ")

	if !strings.Contains(args, "hostfwd=tcp::2299-:22") {
		t.Errorf("ssh forward missing; got %q", args)
	}
	if !strings.Contains(args, "hostfwd=tcp::45000-:6443") {
		t.Errorf("resolved rt.ExtraPortForwards not rendered; got %q", args)
	}
	// The R5 assertion: neither the `auto` sentinel nor the raw spec host port
	// leaks into the argv — the direct spec.Network.PortForwards read is gone.
	if strings.Contains(args, "auto") {
		t.Errorf("unresolved `auto` sentinel leaked into qemu argv: %q", args)
	}
	if strings.Contains(args, "16443") {
		t.Errorf("raw spec.Network.PortForwards host port 16443 rendered — the direct-spec read was NOT deleted: %q", args)
	}
}
