package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestSpecCandyAdapter_HasInit_PerInitLookup is the W9 mutation-site-inventory finding's
// regression proof: specCandyAdapter.HasInit(initName) used to IGNORE initName entirely,
// approximating "does this candy trigger init system X" as `len(ServiceFiles)>0 ||
// len(Service)>0` — true for ANY init the candy uses, whether or not it matches X. The
// FIX reads CandyView.InitSystems[initName] directly (the same per-init map the live
// *Candy.InitSystems field carries, host-completed by PopulateCandyInitSystem).
//
// The FAKE and the REAL diverge exactly on a candy with a PACKAGED-ONLY service:
// entry (use_packaged:, no exec: sibling) whose target init does NOT support packaged
// services (supervisord: service_schema.supports_packaged: false, e.g.
// candy/qemu-guest-agent in the main repo) — the FAKE sees Service non-empty and
// answers true for ANY initName asked, while the REAL correctly answers false for an
// init that entry never triggers.
func TestSpecCandyAdapter_HasInit_PerInitLookup(t *testing.T) {
	m := spec.CandyModel{
		Name: "qemu-guest-agent-like",
		// ServiceFiles empty (no bundled *.service file — the unit ships in the OS
		// package), Service carries ONE packaged-only entry — the exact shape that
		// fooled the old fake.
		Service: []spec.CandyService{{Name: "qemu-guest-agent", UsePackaged: "qemu-guest-agent.service"}},
	}
	v := spec.CandyView{
		Name: "qemu-guest-agent-like",
		// InitSystems is the host-completed per-init map (PopulateCandyInitSystem):
		// this candy's ONLY entry is packaged, and supervisord doesn't support
		// packaged services, so it correctly triggers NEITHER init here — the
		// candy relies entirely on the OS package's own systemd unit, not on
		// charly's init-fragment assembly at all.
		InitSystems: map[string]bool{},
	}
	adapter := NewSpecCandyModel(m, v)

	if got := adapter.HasInit("supervisord"); got {
		t.Errorf("HasInit(supervisord) = %v, want false (packaged-only entry; supervisord does not support packaged services) — "+
			"the OLD fake (len(ServiceFiles)>0 || len(Service)>0) would have wrongly answered true here", got)
	}
	if got := adapter.HasInit("systemd"); got {
		t.Errorf("HasInit(systemd) = %v, want false (this candy's InitSystems carries neither entry, matching supervisord not systemd either)", got)
	}

	// Positive case: a candy that DOES trigger a specific init (an exec: entry, e.g.
	// charly-mcp's supervised service) must answer true for THAT init and false for
	// an unrelated one — proving the lookup is genuinely per-name, not just
	// always-false.
	v2 := spec.CandyView{
		Name:        "charly-mcp-like",
		InitSystems: map[string]bool{"supervisord": true},
	}
	adapter2 := NewSpecCandyModel(spec.CandyModel{Name: "charly-mcp-like"}, v2)
	if got := adapter2.HasInit("supervisord"); !got {
		t.Errorf("HasInit(supervisord) = %v, want true (InitSystems[supervisord] is set)", got)
	}
	if got := adapter2.HasInit("systemd"); got {
		t.Errorf("HasInit(systemd) = %v, want false (InitSystems carries only supervisord)", got)
	}
}
