package deploykit

import (
	"reflect"
	"testing"
)

// The pure topo-sort tests, relocated with topoSort/topoLevels from charly/graph.go
// into sdk/deploykit (P8). They operate on plain map[string][]string graphs.

func TestTopoLevels(t *testing.T) {
	tests := []struct {
		name    string
		graph   map[string][]string
		want    [][]string
		wantErr bool
	}{
		{
			name: "linear chain",
			graph: map[string][]string{
				"a": nil,
				"b": {"a"},
				"c": {"b"},
			},
			want: [][]string{{"a"}, {"b"}, {"c"}},
		},
		{
			name: "two independent roots",
			graph: map[string][]string{
				"a": nil,
				"b": nil,
				"c": {"a"},
				"d": {"b"},
			},
			want: [][]string{{"a", "b"}, {"c", "d"}},
		},
		{
			name: "diamond dependency",
			graph: map[string][]string{
				"a": nil,
				"b": {"a"},
				"c": {"a"},
				"d": {"b", "c"},
			},
			want: [][]string{{"a"}, {"b", "c"}, {"d"}},
		},
		{
			name: "single node",
			graph: map[string][]string{
				"a": nil,
			},
			want: [][]string{{"a"}},
		},
		{
			name: "cycle",
			graph: map[string][]string{
				"a": {"b"},
				"b": {"a"},
			},
			wantErr: true,
		},
		{
			name: "wide first level",
			graph: map[string][]string{
				"a": nil,
				"b": nil,
				"c": nil,
				"d": {"a", "b"},
				"e": {"c"},
			},
			want: [][]string{{"a", "b", "c"}, {"d", "e"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			levels, err := topoLevels(tt.graph)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(levels, tt.want) {
				t.Errorf("topoLevels() = %v, want %v", levels, tt.want)
			}
		})
	}
}

func TestTopoSortDeterministic(t *testing.T) {
	// Run multiple times to ensure deterministic output
	graph := map[string][]string{
		"a": nil,
		"b": nil,
		"c": {"a"},
		"d": {"a", "b"},
	}

	first, err := topoSort(graph)
	if err != nil {
		t.Fatalf("topoSort() error = %v", err)
	}

	for range 10 {
		result, err := topoSort(graph)
		if err != nil {
			t.Fatalf("topoSort() error = %v", err)
		}
		if !reflect.DeepEqual(result, first) {
			t.Errorf("non-deterministic output: got %v, first was %v", result, first)
		}
	}
}
