package deploykit

// fill_box_plans_diamond_test.go — the diamond/multi-alias box-plan fold.
//
// FillBoxPlans must record every namespace PATH of a shared config, not only the first. A global
// pointer-visited guard recorded only the first alias (map-order dependent), so a box plan reached
// under a later alias (a diamond import, or one repo at `arch` + `cachyos.arch`) was missing.

import (
	"sort"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestFillBoxPlans_DiamondRecordsAllPaths(t *testing.T) {
	// One shared namespace config `c` owning box `app`, mounted at both `a.c` and `b.c`.
	shared := &spec.Config{Box: spec.BoxMap{
		"app": spec.EncodeBox(spec.BoxConfig{
			Candy:       []string{"redis"},
			Description: "the app box",
			Plan: []spec.Step{{Check: "the app answers", Op: spec.Op{
				Plugin: "port", PluginInput: map[string]any{"port": 6379},
			}}},
		}),
	}}
	cfg := &spec.Config{
		Namespaces: map[string]*spec.Config{
			"a": {Namespaces: map[string]*spec.Config{"c": shared}},
			"b": {Namespaces: map[string]*spec.Config{"c": shared}},
		},
	}
	layers := map[string]CandyModel{
		"redis": NewSpecCandyModel(
			spec.CandyModel{Plan: []spec.Step{{Check: "redis listens", Op: spec.Op{
				Plugin: "port", PluginInput: map[string]any{"port": 6379},
			}}}},
			spec.CandyView{Name: "redis", Description: "redis server"},
		),
	}
	out := map[string][]spec.Step{}
	FillBoxPlans(cfg, layers, "", out, map[*spec.Config]bool{})

	for _, want := range []string{"a.c.app", "b.c.app"} {
		if _, ok := out[want]; !ok {
			t.Fatalf("box plan %q missing — the diamond's later alias was not folded; got %v", want, sortedPlanKeys(out))
		}
	}
}

func TestFillBoxPlans_SelfCycleTerminates(t *testing.T) {
	self := &spec.Config{}
	self.Namespaces = map[string]*spec.Config{"self": self}
	out := map[string][]spec.Step{}
	FillBoxPlans(self, nil, "", out, map[*spec.Config]bool{}) // must return, not hang
}

func sortedPlanKeys(m map[string][]spec.Step) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
