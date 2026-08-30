package kit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVmKnownHostsFile_IsoRecordsNothing(t *testing.T) {
	got := VmKnownHostsFile("iso", "/state/charly-omarchy-vm")
	if got != os.DevNull {
		t.Fatalf("an iso guest must record no host key, got %q", got)
	}
}

// NEGATIVE CONTROL. The tempting simplification is to disable host-key recording for every
// VM — it makes the iso case work and nothing visibly break. It also silently removes
// host-key continuity from every guest that has a stable identity from first boot, which is
// a security property, not a convenience.
func TestVmKnownHostsFile_OtherKindsKeepTheirKnownHosts(t *testing.T) {
	for _, kind := range []string{"cloud_image", "bootc", "bootstrap", "clone", ""} {
		got := VmKnownHostsFile(kind, "/state/charly-vm")
		want := filepath.Join("/state/charly-vm", "known_hosts")
		if got != want {
			t.Errorf("kind %q: host-key recording was disabled for a VM with ONE stable identity: got %q, want %q",
				kind, got, want)
		}
	}
}

// The state dir is per-DOMAIN, so two beds sharing one kind:vm entity must never be handed
// the same known_hosts path — their guests are different machines with different keys.
func TestVmKnownHostsFile_IsPerStateDir(t *testing.T) {
	a := VmKnownHostsFile("cloud_image", "/state/charly-bed-a")
	b := VmKnownHostsFile("cloud_image", "/state/charly-bed-b")
	if a == b {
		t.Fatalf("two domains got the same known_hosts path (%q) — one bed's host key would "+
			"be checked against another bed's guest", a)
	}
}
