package deploykit

import (
	"errors"
	"reflect"
	"testing"

	"github.com/opencharly/sdk/buildkit"
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

func TestResolveBoxGraphBuilderBackedImageWithoutBase(t *testing.T) {
	boxes := map[string]*buildkit.ResolvedBox{
		"bootstrap-builder": {Name: "bootstrap-builder", IsExternalBase: true},
		"bootstrap-image": {
			Name:                  "bootstrap-image",
			From:                  "builder:pacstrap",
			BootstrapBuilderImage: "bootstrap-builder",
		},
	}

	deps := BoxDirectDeps("bootstrap-image", boxes["bootstrap-image"], boxes, false)
	if want := []string{"bootstrap-builder"}; !reflect.DeepEqual(deps, want) {
		t.Fatalf("BoxDirectDeps() = %v, want %v", deps, want)
	}

	order, err := ResolveBoxOrder(boxes, nil)
	if err != nil {
		t.Fatalf("ResolveBoxOrder() error = %v", err)
	}
	if want := []string{"bootstrap-builder", "bootstrap-image"}; !reflect.DeepEqual(order, want) {
		t.Errorf("ResolveBoxOrder() = %v, want %v", order, want)
	}

	levels, err := ResolveBoxLevels(boxes, nil)
	if err != nil {
		t.Fatalf("ResolveBoxLevels() error = %v", err)
	}
	if want := [][]string{{"bootstrap-builder"}, {"bootstrap-image"}}; !reflect.DeepEqual(levels, want) {
		t.Errorf("ResolveBoxLevels() = %v, want %v", levels, want)
	}
}

func TestResolveBoxGraphExcludesBaseOutsideProjectedBoxSet(t *testing.T) {
	boxes := map[string]*buildkit.ResolvedBox{
		"local-image": {Name: "local-image", Base: "arch.arch"},
	}

	if deps := BoxDirectDeps("local-image", boxes["local-image"], boxes, false); len(deps) != 0 {
		t.Fatalf("BoxDirectDeps() = %v, want no dependency outside projected box set", deps)
	}

	order, err := ResolveBoxOrder(boxes, nil)
	if err != nil {
		t.Fatalf("ResolveBoxOrder() error = %v", err)
	}
	if want := []string{"local-image"}; !reflect.DeepEqual(order, want) {
		t.Errorf("ResolveBoxOrder() = %v, want %v", order, want)
	}

	levels, err := ResolveBoxLevels(boxes, nil)
	if err != nil {
		t.Fatalf("ResolveBoxLevels() error = %v", err)
	}
	if want := [][]string{{"local-image"}}; !reflect.DeepEqual(levels, want) {
		t.Errorf("ResolveBoxLevels() = %v, want %v", levels, want)
	}
}

func TestResolveBoxGraphStillReportsRealCycle(t *testing.T) {
	boxes := map[string]*buildkit.ResolvedBox{
		"a": {Name: "a", Base: "b"},
		"b": {Name: "b", Base: "a"},
	}

	for name, resolve := range map[string]func() error{
		"order":  func() error { _, err := ResolveBoxOrder(boxes, nil); return err },
		"levels": func() error { _, err := ResolveBoxLevels(boxes, nil); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := resolve()
			var cycleErr *CycleError
			if !errors.As(err, &cycleErr) {
				t.Fatalf("error = %v, want *CycleError", err)
			}
			if len(cycleErr.Cycle) == 0 {
				t.Fatal("CycleError.Cycle is empty for a real cycle")
			}
		})
	}
}
