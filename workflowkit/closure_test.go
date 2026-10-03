package workflowkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestClosureOrderingAndDedup(t *testing.T) {
	tasks := map[string]spec.Task{
		"c": {DependsOn: []string{"b"}},
		"b": {DependsOn: []string{"a"}},
		"a": {},
	}
	got, err := Closure(tasks, []string{"c", "b"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Closure = %v, want %v", got, want)
	}
}

func TestClosureCycleNamesTheChain(t *testing.T) {
	tasks := map[string]spec.Task{
		"a": {DependsOn: []string{"b"}},
		"b": {DependsOn: []string{"a"}},
	}
	_, err := Closure(tasks, []string{"a"})
	if err == nil {
		t.Fatal("Closure accepted a dependency cycle")
	}
	if got := err.Error(); !strings.HasPrefix(got, "task dependency cycle: ") || !strings.Contains(got, " -> ") {
		t.Errorf("cycle error %q must name the chain as `task dependency cycle: a -> b -> a`", got)
	}
}

func TestClosureUnknownDepAndRoot(t *testing.T) {
	tasks := map[string]spec.Task{"a": {DependsOn: []string{"ghost"}}}
	if _, err := Closure(tasks, []string{"a"}); err == nil {
		t.Error("Closure accepted an unknown dependency")
	}
	if _, err := Closure(tasks, []string{"ghost"}); err == nil {
		t.Error("Closure accepted an unknown root")
	}
}

func TestSortedNames(t *testing.T) {
	tasks := map[string]spec.Task{"b": {}, "a": {}, "c": {}}
	if got := strings.Join(SortedNames(tasks), ","); got != "a,b,c" {
		t.Errorf("SortedNames = %q", got)
	}
}
