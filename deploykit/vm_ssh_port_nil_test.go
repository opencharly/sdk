package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestResolveVmSshPortNilConfigNoPanic gates the plugin-process nil-safety (RCA
// 2026-09-06): in an out-of-process plugin (check live, deploy-vm) the DeployStateHost
// is never registered, so LoadDeployConfigForRead returns NIL — the resolver must treat
// that as "no persisted state" (a fresh port), NOT dereference nil. Removing the nil
// guard panics the test.
func TestResolveVmSshPortNilConfigNoPanic(t *testing.T) {
	sp := &spec.ResolvedVm{
		SSH: &spec.VmSsh{PortAuto: true},
	}
	port, err := ResolveVmSshPort(sp, "some-vm")
	if err != nil {
		t.Fatalf("ResolveVmSshPort with a nil config must not error: %v", err)
	}
	if port <= 0 {
		t.Fatalf("a fresh port must be allocated, got %d", port)
	}
}
