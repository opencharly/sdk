package loaderkit

// materialize_wire_diamond_test.go — the diamond/multi-alias PluginKinds round-trip.
//
// capturePluginKindsSeen/restorePluginKindsSeen must record + restore every namespace PATH of a
// shared *UnifiedFile, not only the first. A global pointer-identity guard dropped every later
// alias's PluginKinds, so a namespaced bed mounted at such an alias false-failed "not defined".

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestMarshalMaterialized_DiamondPreservesAllPaths: one shared namespace value mounted at both
// `a.c` and `b.c` (the diamond) must carry its PluginKinds under BOTH paths after a
// marshal→unmarshal round-trip.
func TestMarshalMaterialized_DiamondPreservesAllPaths(t *testing.T) {
	vmBody := json.RawMessage(`{"vm":{"source":{"kind":"iso"}}}`)
	shared := &spec.UnifiedFile{
		PluginKinds: map[string]map[string]json.RawMessage{"vm": {"tmpl": vmBody}},
	}
	root := &spec.UnifiedFile{
		Namespaces: map[string]*spec.UnifiedFile{
			"a": {Namespaces: map[string]*spec.UnifiedFile{"c": shared}},
			"b": {Namespaces: map[string]*spec.UnifiedFile{"c": shared}},
		},
	}
	data, err := MarshalMaterialized(root)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got spec.UnifiedFile
	if err := UnmarshalMaterialized(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ac := got.Namespaces["a"].Namespaces["c"]
	bc := got.Namespaces["b"].Namespaces["c"]
	if len(ac.PluginKinds["vm"]) != 1 {
		t.Fatalf("a.c lost its PluginKinds (vm=%d)", len(ac.PluginKinds["vm"]))
	}
	if len(bc.PluginKinds["vm"]) != 1 {
		t.Fatalf("b.c lost its PluginKinds (the diamond's later alias) vm=%d keys=%v", len(bc.PluginKinds["vm"]), sortedKinds(bc.PluginKinds))
	}
}

// TestCapturePluginKinds_SelfCycleTerminates proves the ancestor guard still terminates a
// self-referential namespace graph (the mutual-import cycle the original pointer guard existed
// for). It calls capturePluginKinds directly (the marshal path itself rejects a literal pointer
// cycle at the json layer, independent of this guard).
func TestCapturePluginKinds_SelfCycleTerminates(t *testing.T) {
	self := &spec.UnifiedFile{PluginKinds: map[string]map[string]json.RawMessage{"vm": {}}}
	self.Namespaces = map[string]*spec.UnifiedFile{"self": self}
	out := pluginKindsByPath{}
	capturePluginKinds(self, "", out) // must return, not hang/overflow
	if len(out) == 0 {
		t.Fatal("capturePluginKinds recorded nothing")
	}
}

func sortedKinds(m map[string]map[string]json.RawMessage) []string {
	var o []string
	for k := range m {
		o = append(o, k)
	}
	sort.Strings(o)
	return o
}
