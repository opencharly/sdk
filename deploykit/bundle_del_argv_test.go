package deploykit

import (
	"reflect"
	"testing"
)

func TestBundleDelArgv(t *testing.T) {
	got := BundleDelArgv("myapp")
	want := []string{"bundle", "del", "myapp", "--assume-yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BundleDelArgv(%q) = %v, want %v", "myapp", got, want)
	}
}
