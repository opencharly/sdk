package kit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestResolveVmSshPortNilSpec gates the NIL-SPEC guard (RCA 2026-09-06): the live-check
// deploy-hop can miss the template, producing a nil spec — the resolver must fall back to
// the in-guest default (22), never panic. Removing the guard panics the test.
func TestResolveVmSshPortNilSpec(t *testing.T) {
	port, err := ResolveVmSshPort(nil, "some-vm", 0)
	if err != nil {
		t.Fatalf("ResolveVmSshPort(nil, ...) must not error: %v", err)
	}
	if port != 22 {
		t.Fatalf("a nil spec falls back to the in-guest default, got %d (want 22)", port)
	}
}

// TestResolveVmSshPortDefault keeps the documented default semantics: a non-nil spec with
// no SSH block resolves to 2222 (neither port_auto nor a fixed port).
func TestResolveVmSshPortDefault(t *testing.T) {
	port, err := ResolveVmSshPort(&spec.ResolvedVm{}, "some-vm", 0)
	if err != nil {
		t.Fatalf("ResolveVmSshPort(&spec.ResolvedVm{}, ...) must not error: %v", err)
	}
	if port != 2222 {
		t.Fatalf("the documented default is 2222, got %d", port)
	}
}
