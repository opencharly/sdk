package kit

import (
	"reflect"
	"testing"
)

func TestExpandTestVars(t *testing.T) {
	env := map[string]string{
		"HOME":           "/home/user",
		"HOST_PORT:6379": "16379",
		"VOLUME_PATH:ws": "/tmp/ws",
		"CONTAINER_IP":   "10.88.0.12",
	}
	in := "ls ${HOME} && redis-cli -h ${CONTAINER_IP} -p ${HOST_PORT:6379} ${VOLUME_PATH:ws} ${UNKNOWN} ${HOST_PORT:9999}"
	out, missing := ExpandTestVars(in, env)

	want := "ls /home/user && redis-cli -h 10.88.0.12 -p 16379 /tmp/ws ${UNKNOWN} ${HOST_PORT:9999}"
	if out != want {
		t.Errorf("out =\n  %q\nwant\n  %q", out, want)
	}
	// Missing order-preserving, deduplicated
	wantMissing := []string{"UNKNOWN", "HOST_PORT:9999"}
	if !reflect.DeepEqual(missing, wantMissing) {
		t.Errorf("missing = %v, want %v", missing, wantMissing)
	}
}

// TestVarRefs returns deduplicated refs in encounter order.
func TestTestVarRefs(t *testing.T) {
	got := TestVarRefs("${A} ${B:x} ${A} ${C} ${B:y}")
	want := []string{"A", "B:x", "C", "B:y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// IsRuntimeOnlyVar classifies deploy-only variable keys correctly.
func TestIsRuntimeOnlyVar(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"HOME", false},
		{"USER", false},
		{"DNS", false},
		{"HOST_PORT:6379", true},
		{"VOLUME_PATH:workspace", true},
		{"VOLUME_CONTAINER_PATH:workspace", true},
		{"CONTAINER_IP", true},
		{"CONTAINER_NAME", true},
		{"INSTANCE", true},
		{"ENV_TOKEN", true},
		{"ENV_ANYTHING", true},
		{"DEPLOY_NAME", true},
		{"HOST:driver", true},
	}
	for _, tc := range cases {
		if got := IsRuntimeOnlyVar(tc.key); got != tc.want {
			t.Errorf("IsRuntimeOnlyVar(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

// ExpandAnyVars walks nested plugin-input maps/lists in place.
func TestExpandAnyVars_Nested(t *testing.T) {
	env := map[string]string{"HOST_PORT:80": "8080", "HOME": "/h"}
	in := map[string]any{
		"url":    "http://x:${HOST_PORT:80}/p",
		"nested": []any{"${HOME}", map[string]any{"k": "${MISSING}"}},
		"num":    42,
	}
	out, missing := ExpandAnyVars(in, env)
	m := out.(map[string]any)
	if m["url"] != "http://x:8080/p" {
		t.Errorf("url = %v", m["url"])
	}
	if m["num"] != 42 {
		t.Errorf("non-string scalar must pass through, got %v", m["num"])
	}
	nested := m["nested"].([]any)
	if nested[0] != "/h" {
		t.Errorf("nested[0] = %v", nested[0])
	}
	found := false
	for _, k := range missing {
		if k == "MISSING" {
			found = true
		}
	}
	if !found {
		t.Errorf("MISSING should be reported unresolved, got %v", missing)
	}
}

func TestCollectAnyStrings(t *testing.T) {
	got := CollectAnyStrings(map[string]any{
		"a": "one",
		"b": []any{"two", map[string]any{"c": "three"}},
		"n": 7,
	})
	// order over a map is non-deterministic; check membership + count
	if len(got) != 3 {
		t.Fatalf("want 3 strings, got %v", got)
	}
	set := map[string]bool{}
	for _, s := range got {
		set[s] = true
	}
	for _, want := range []string{"one", "two", "three"} {
		if !set[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
}
