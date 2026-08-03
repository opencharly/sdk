package kit

import (
	"reflect"
	"testing"
)

// TestFilterHostVars: relocated from charly/check_members_test.go (#55 decoupling
// cone, Batch D) — asserts FilterHostVars' own behavior directly, zero charly
// coupling. Only ${HOST:…} keys are selected — the ones whose unresolution must FAIL
// (not skip) a check. ${HOST_PORT} (a distinct var) is NOT.
func TestFilterHostVars(t *testing.T) {
	got := FilterHostVars([]string{"HOST:web:8080", "HOST_PORT:8080", "HOST:web", "USER"})
	want := []string{"HOST:web:8080", "HOST:web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FilterHostVars = %v, want %v", got, want)
	}
	if got := FilterHostVars([]string{"HOST_PORT:8080", "USER"}); len(got) != 0 {
		t.Errorf("FilterHostVars with no host vars = %v, want empty", got)
	}
}
