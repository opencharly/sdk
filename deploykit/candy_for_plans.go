package deploykit

import "github.com/opencharly/spec/spec"

// candy_for_plans.go — the pure plan→candy SELECTION shared (R3) by charly-core's host CandyForPlan
// (which pairs it with the host-only ScanAllCandyWithConfig loader scan) AND candy/plugin-bundle's
// plugin-side secret/artifact resolution (which pairs it with the candy set it already holds in the
// resolved-project envelope — no scan). Both need the SAME "which candies back this compiled plan
// set, in dependency order" decision; only WHERE the candy map comes from differs (#55 K4).

// SelectCandiesForPlans returns the candies backing plans — each plan's CandiesIncluded (topo order)
// then its own Candy — deduped, in first-seen order, resolved against the supplied candies map. A
// name absent from candies is skipped (the caller's map is authoritative for what exists). Pure: no
// scan, no I/O — the candies map is whatever the caller already resolved (a host ScanAllCandyWithConfig
// result, or an envelope's CandyModels).
func SelectCandiesForPlans(plans []*spec.InstallPlan, candies map[string]spec.CandyReader) []spec.CandyReader {
	seen := map[string]bool{}
	var ordered []spec.CandyReader
	pick := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		if l, ok := candies[name]; ok {
			ordered = append(ordered, l)
		}
	}
	for _, p := range plans {
		if p == nil {
			continue
		}
		for _, name := range p.CandiesIncluded {
			pick(name)
		}
		pick(p.Candy)
	}
	return ordered
}
