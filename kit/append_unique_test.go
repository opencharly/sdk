package kit

import (
	"reflect"
	"testing"
)

// Migrated from deploykit/security_test.go's TestAppendUniqueString (W9/K1 R3
// fix: deploykit's own byte-identical copy of this helper is gone; deploykit
// now aliases this function via kit_aliases.go, so the test belongs on the
// owning implementation here, not the consuming alias).
func TestAppendUnique(t *testing.T) {
	result := AppendUnique([]string{"a", "b"}, "b", "c", "a", "d")
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("AppendUnique = %v, want %v", result, want)
	}
}
