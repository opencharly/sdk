package workflowkit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/opencharly/spec/spec"
)

// closure.go — the depends_on resolution: a topological closure (dependency-first)
// with cycle detection. Moved from plugin-task (R3: ONE implementation; the
// plugin-task CLI and the workflow lowering both call it).

// Closure returns the tasks to run for `names`, dependency-first, deduplicated, with
// every task's depends_on recursively included. A cycle is a hard error naming the
// chain. The returned order is stable: a depth-first post-order over the declared
// depends_on lists.
func Closure(tasks map[string]spec.Task, names []string) ([]string, error) {
	var order []string
	state := map[string]int{} // 0 unvisited, 1 in-progress, 2 done
	var stack []string

	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("task dependency cycle: %s", strings.Join(append(stack, name), " -> "))
		case 2:
			return nil
		}
		t, ok := tasks[name]
		if !ok {
			return fmt.Errorf("task %q depends on unknown task %q", stack[len(stack)-1], name)
		}
		state[name] = 1
		stack = append(stack, name)
		for _, dep := range t.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		order = append(order, name)
		return nil
	}

	// visit roots in the caller's order; a missing root is reported against itself.
	for _, n := range names {
		if _, ok := tasks[n]; !ok {
			return nil, fmt.Errorf("unknown task %q (declared tasks: %s)", n, strings.Join(SortedNames(tasks), ", "))
		}
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// SortedNames returns a copy of the task names in lexical order.
func SortedNames(tasks map[string]spec.Task) []string {
	out := make([]string, 0, len(tasks))
	for name := range tasks {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
