package deploykit

import (
	"sort"
	"strconv"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// ports_collect.go — the box-PORTS aggregator (relocated from charly/ports.go in the
// core-min wave-3 build-cluster split). CollectBoxPorts is a PURE candy-chain walk over
// (spec.Config, layers, boxName) — no charly-core coupling (spec.Config is the loader's own
// type, BoxCandyChain + kit.StripPortSuffix are sdk mechanisms), so it belongs beside its
// sibling chain-collectors (BoxCandyChain, MergeCandySecurity, CollectDescriptions) here in
// deploykit, shared by the host projector (resolved_project_host.go), the build render, and
// box-inspect — host today, an out-of-process build plugin tomorrow.

// CollectBoxPorts returns an image's aggregated exposed container ports, walking the box's
// full candy chain and deduplicating by port number (with a "/proto" suffix when non-http).
func CollectBoxPorts(cfg *spec.Config, layers map[string]CandyModel, boxName string) ([]string, error) {
	names, err := BoxCandyChain(cfg, layers, boxName)
	if err != nil {
		return nil, err
	}
	type portEntry struct {
		cp    int
		proto string
	}
	seen := map[int]bool{}
	var entries []portEntry
	for _, candyName := range names {
		layer, ok := layers[candyName]
		if !ok || !layer.HasPorts() {
			continue
		}
		cports, perr := layer.Port()
		if perr != nil {
			continue
		}
		for _, cpStr := range cports {
			clean, proto := kit.StripPortSuffix(cpStr)
			cp, aerr := strconv.Atoi(clean)
			if aerr != nil || cp <= 0 || cp > 65535 || seen[cp] {
				continue
			}
			seen[cp] = true
			entries = append(entries, portEntry{cp: cp, proto: proto})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].cp < entries[j].cp })
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		s := strconv.Itoa(e.cp)
		if e.proto != "" {
			s += "/" + e.proto
		}
		out = append(out, s)
	}
	return out, nil
}
