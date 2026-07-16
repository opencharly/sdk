package kit

import (
	"reflect"
	"sort"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestSplitHostKey / TestCollectHostRefs: relocated from
// charly/check_members_test.go (P12a follow-up).

func TestSplitHostKey(t *testing.T) {
	cases := []struct {
		key, name, arg string
		ok             bool
	}{
		{"HOST:web", "HOST", "web", true},
		{"HOST:web:8080", "HOST", "web:8080", true},
		{"HOST", "HOST", "", false},
	}
	for _, c := range cases {
		name, arg, ok := SplitHostKey(c.key)
		if name != c.name || arg != c.arg || ok != c.ok {
			t.Errorf("SplitHostKey(%q) = (%q,%q,%v), want (%q,%q,%v)", c.key, name, arg, ok, c.name, c.arg, c.ok)
		}
	}
}

// TestCollectHostRefs scans every check string field for ${HOST:…} refs and
// returns exactly those (not other parameterized vars like ${HOST_PORT}).
func TestCollectHostRefs(t *testing.T) {
	checks := []spec.Op{
		{Plugin: "cdp", PluginInput: map[string]any{"method": "open", "url": "http://${HOST:web}:8080"}},
		{Plugin: "command", PluginInput: map[string]any{"command": "curl http://${HOST:web:8080}/health"}},
		// addr/http are plugin verbs now — their refs live in plugin_input (CollectHostRefs
		// scans it via CollectAnyStrings). The addr HOST_PORT is NOT a cross-member ref; the
		// http ${HOST:web} is a duplicate of the cdp one.
		{Plugin: "addr", PluginInput: map[string]any{"addr": "127.0.0.1:${HOST_PORT:8080}"}},
		{Plugin: "http", PluginInput: map[string]any{"http": "http://${HOST:web}/"}},
	}
	got := CollectHostRefs(checks)
	sort.Strings(got)
	want := []string{"HOST:web", "HOST:web:8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectHostRefs = %v, want %v", got, want)
	}
}

// TestCloseHostCleanups: nil/empty-safe, calls every non-nil cleanup exactly once.
func TestCloseHostCleanups(t *testing.T) {
	CloseHostCleanups(nil)
	CloseHostCleanups([]func(){})
	calls := 0
	CloseHostCleanups([]func(){nil, func() { calls++ }, func() { calls++ }})
	if calls != 2 {
		t.Errorf("CloseHostCleanups called %d cleanups, want 2", calls)
	}
}
