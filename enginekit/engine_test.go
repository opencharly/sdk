package enginekit

// engine_test.go — ContainerSnapshot.HostPortFor coverage, ported from charly/status_test.go (K6:
// this was the sole coverage of an sdk-only method that had been sitting in a charly-core test
// file with no core coupling at all).

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestHostPortFor_Bridge(t *testing.T) {
	snap := &ContainerSnapshot{
		NetworkMode: "charly",
		Ports: []spec.PortMapping{
			{HostIP: "127.0.0.1", HostPort: 9240, CtrPort: 9222, Proto: "tcp"},
			{HostIP: "0.0.0.0", HostPort: 5900, CtrPort: 5900, Proto: "tcp"},
		},
	}
	ip, port, ok := snap.HostPortFor(9222, "tcp")
	if !ok || ip != "127.0.0.1" || port != 9240 {
		t.Errorf("9222: ok=%v ip=%q port=%d", ok, ip, port)
	}
	ip, port, ok = snap.HostPortFor(5900, "tcp")
	if !ok || ip != "127.0.0.1" || port != 5900 {
		t.Errorf("5900 (0.0.0.0 → 127.0.0.1): ok=%v ip=%q port=%d", ok, ip, port)
	}
	if _, _, ok := snap.HostPortFor(9999, "tcp"); ok {
		t.Errorf("9999 should not be published")
	}
}

func TestHostPortFor_HostNetwork(t *testing.T) {
	snap := &ContainerSnapshot{NetworkMode: "host"}
	ip, port, ok := snap.HostPortFor(9222, "tcp")
	if !ok || ip != "127.0.0.1" || port != 9222 {
		t.Errorf("host-net 9222: ok=%v ip=%q port=%d", ok, ip, port)
	}
}
