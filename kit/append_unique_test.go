package kit

import (
	"reflect"
	"testing"
)

func TestAppendUnique(t *testing.T) {
	result := AppendUnique([]string{"a", "b"}, "b", "c", "a", "d")
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("AppendUnique = %v, want %v", result, want)
	}
}
