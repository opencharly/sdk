package deploykit

import (
	"reflect"
	"testing"
)

func TestDeployDelArgv(t *testing.T) {
	got := DeployDelArgv("myapp")
	want := []string{"deploy", "del", "myapp", "--assume-yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DeployDelArgv(%q) = %v, want %v", "myapp", got, want)
	}
}
