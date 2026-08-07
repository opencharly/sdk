package deploykit

import (
	"reflect"
	"testing"
)

func TestFleetDelArgv(t *testing.T) {
	got := FleetDelArgv("myapp")
	want := []string{"fleet", "del", "myapp", "--assume-yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FleetDelArgv(%q) = %v, want %v", "myapp", got, want)
	}
}
