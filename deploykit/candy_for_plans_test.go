package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestSelectCandiesForPlans covers the pure plan→candy pick shared (#55 K4) by charly's host
// CandyForPlan and candy/plugin-fleet's plugin-side secret/artifact resolution: CandiesIncluded
// (topo order) then per-plan Candy, deduped in first-seen order, resolved against the supplied map;
// a name absent from the map is skipped. This is the code the collapsed deploy-candy-secrets /
// deploy-artifacts-retrieve seams used to run host-side — a regression here misroutes candy secrets.
func TestSelectCandiesForPlans(t *testing.T) {
	candies := map[string]spec.CandyReader{
		"a": testCandy("a", spec.CandyModel{}, spec.CandyView{}),
		"b": testCandy("b", spec.CandyModel{}, spec.CandyView{}),
		"c": testCandy("c", spec.CandyModel{}, spec.CandyView{}),
	}
	plans := []*spec.InstallPlan{
		{CandiesIncluded: []string{"a", "b"}}, // a, b
		nil,                                   // skipped (nil-safe)
		{CandiesIncluded: []string{"b"}, Candy: "c"}, // b dedup, c added
		{Candy: "missing"},                           // absent from map → skipped
	}

	got := SelectCandiesForPlans(plans, candies)

	names := make([]string, 0, len(got))
	for _, c := range got {
		names = append(names, c.GetName())
	}
	want := []string{"a", "b", "c"}
	if len(names) != len(want) {
		t.Fatalf("SelectCandiesForPlans returned %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, names[i], want[i], names)
		}
	}
}

// TestSelectCandiesForPlans_Empty pins the empty-input edge: nil plans / nil map → empty, no panic.
func TestSelectCandiesForPlans_Empty(t *testing.T) {
	if got := SelectCandiesForPlans(nil, nil); len(got) != 0 {
		t.Errorf("SelectCandiesForPlans(nil, nil) = %v, want empty", got)
	}
}
