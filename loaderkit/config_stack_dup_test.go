package loaderkit

// config_stack_dup_test.go — the within-layer duplicate-key rejection + the
// cross-layer merge precedence (restored from main's version; the merge
// conflict resolution had dropped them). The guard preserves the parse-level
// "duplicate top-level entity name" contract message (parse.go).

import (
	"strings"
	"testing"
)

// TestConfigStack_RejectsDuplicateKeysWithinLayer — a SINGLE document whose
// top-level mapping repeats a key (the same entity name declared twice, e.g.
// `redis:` as a candy AND as a local node) must ERROR at the stack merge.
// The raw yaml.Node round-trip PRESERVES the duplicate (the merge idx-map is
// what would silently collapse it last-wins), and collapsing it would make the
// parse-level "duplicate top-level entity name" contract (parse.go) unreachable
// — the charly TestCrossKindNameReuse pin.
func TestConfigStack_RejectsDuplicateKeysWithinLayer(t *testing.T) {
	dupDoc := []byte("version: 2026.250.0731\nredis:\n    candy:\n        base: fedora\nredis:\n    local:\n        candy: [redis]\n")
	_, err := mergeConfigStackRaw([][]byte{dupDoc})
	if err == nil {
		t.Fatal("mergeConfigStackRaw silently collapsed a duplicate top-level key within one layer; want a duplicate-name error")
	}
	if !strings.Contains(err.Error(), "duplicate top-level entity name") {
		t.Errorf("error = %v, want it to carry the parse-level 'duplicate top-level entity name' contract message", err)
	}
}

// TestConfigStack_CrossLayerDuplicateKeyStillMerges — duplicate keys ACROSS
// layers are the config-stack precedence itself: the same top-level key in two
// different layers merges later-wins WITHOUT an error. Only WITHIN-layer
// duplicates are rejected (the parse contract); cross-layer merging is
// unaffected by the new guard.
func TestConfigStack_CrossLayerDuplicateKeyStillMerges(t *testing.T) {
	layers := [][]byte{
		[]byte("version: 2026.250.0731\nredis:\n    candy:\n        base: fedora\n"),
		[]byte("version: 2026.250.0731\nredis:\n    local:\n        candy: [redis]\n"),
	}
	merged, err := mergeConfigStackRaw(layers)
	if err != nil {
		t.Fatalf("cross-layer duplicate key must still merge (later wins): %v", err)
	}
	s := string(merged)
	if !strings.Contains(s, "local:") || strings.Contains(s, "base: fedora") {
		t.Errorf("later layer should win for the shared key; merged doc = %s", s)
	}
}
