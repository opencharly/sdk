package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestMaterializedWire_PreservesPluginKinds is the regression guard for the R10-bed-caught keystone
// bug: a plain json.Marshal of a spec.UnifiedFile DROPS PluginKinds (json:"-"), so the plugin-side
// witness lost every standalone-template / plugin-kind entity and a kind:check bed's `from:
// <local-template>` false-failed "not defined". MarshalMaterialized/UnmarshalMaterialized must carry
// PluginKinds across byte-identically.
func TestMaterializedWire_PreservesPluginKinds(t *testing.T) {
	src := &spec.UnifiedFile{
		Version: "2026.200.1200",
		RootDir: "/project",
		PluginKinds: map[string]map[string]json.RawMessage{
			"local": {"check-feature-app": json.RawMessage(`{"from":"base"}`)},
			"vm":    {"my-vm": json.RawMessage(`{"backend":"qemu"}`)},
		},
	}

	// FIRST prove the bug the fix addresses: a plain json round-trip DROPS PluginKinds.
	plain, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("plain marshal: %v", err)
	}
	var viaPlain spec.UnifiedFile
	if err := json.Unmarshal(plain, &viaPlain); err != nil {
		t.Fatalf("plain unmarshal: %v", err)
	}
	if viaPlain.PluginKinds != nil {
		t.Fatalf("precondition failed: plain json round-trip UNEXPECTEDLY preserved PluginKinds — the fix would be unnecessary")
	}

	// NOW the fix: MarshalMaterialized/UnmarshalMaterialized preserve PluginKinds.
	data, err := MarshalMaterialized(src)
	if err != nil {
		t.Fatalf("MarshalMaterialized: %v", err)
	}
	var got spec.UnifiedFile
	if err := UnmarshalMaterialized(data, &got); err != nil {
		t.Fatalf("UnmarshalMaterialized: %v", err)
	}
	if got.Version != "2026.200.1200" || got.RootDir != "/project" {
		t.Errorf("scalar fields not preserved: Version=%q RootDir=%q", got.Version, got.RootDir)
	}
	if _, ok := got.PluginKinds["local"]["check-feature-app"]; !ok {
		t.Errorf("PluginKinds[local][check-feature-app] DROPPED — the witness bed regression is back")
	}
	if _, ok := got.PluginKinds["vm"]["my-vm"]; !ok {
		t.Errorf("PluginKinds[vm][my-vm] DROPPED")
	}
}

// TestMaterializedWire_PreservesNamespacedPluginKinds proves the recursive capture/restore keeps a
// MOUNTED-namespace's PluginKinds too (byte-identical at every level, not just the root).
func TestMaterializedWire_PreservesNamespacedPluginKinds(t *testing.T) {
	src := &spec.UnifiedFile{
		Version: "2026.200.1200",
		PluginKinds: map[string]map[string]json.RawMessage{
			"local": {"root-tpl": json.RawMessage(`{}`)},
		},
		Namespaces: map[string]*spec.UnifiedFile{
			"cachyos": {
				RootDir:     "/ns/cachyos",
				PluginKinds: map[string]map[string]json.RawMessage{"vm": {"ns-vm": json.RawMessage(`{"backend":"libvirt"}`)}},
			},
		},
	}
	data, err := MarshalMaterialized(src)
	if err != nil {
		t.Fatalf("MarshalMaterialized: %v", err)
	}
	var got spec.UnifiedFile
	if err := UnmarshalMaterialized(data, &got); err != nil {
		t.Fatalf("UnmarshalMaterialized: %v", err)
	}
	if _, ok := got.PluginKinds["local"]["root-tpl"]; !ok {
		t.Errorf("root PluginKinds dropped")
	}
	ns := got.Namespaces["cachyos"]
	if ns == nil {
		t.Fatalf("namespace cachyos dropped")
	}
	if ns.RootDir != "/ns/cachyos" {
		t.Errorf("namespace RootDir not preserved: %q", ns.RootDir)
	}
	if _, ok := ns.PluginKinds["vm"]["ns-vm"]; !ok {
		t.Errorf("namespaced PluginKinds[vm][ns-vm] DROPPED — recursive capture/restore broken")
	}
}
