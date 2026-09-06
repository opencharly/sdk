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

// TestResolveVmSshPortNilSpec gates the NIL-SPEC guard (RCA 2026-09-06): the live-check
// deploy-hop can miss the template, producing a nil spec — the resolver must fall back to
// the in-guest default (22), never panic. Removing the guard panics the test.
func TestResolveVmSshPortNilSpec(t *testing.T) {
	port, err := ResolveVmSshPort(nil, "some-vm")
	if err != nil {
		t.Fatalf("ResolveVmSshPort(nil, ...) must not error: %v", err)
	}
	if port != 22 {
		t.Fatalf("a nil spec falls back to the in-guest default, got %d (want 22)", port)
	}
}
