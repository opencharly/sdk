package deploykit

import (
	"strings"

	"github.com/opencharly/spec/spec"
)

// hooks_collect.go — the candy-hooks merge logic (W9: MergeCandyHooks) AND — since the core-min
// wave-3 build-cluster split — the full CollectHooks aggregator relocated from charly/hooks.go.
// CollectHooks resolves the box's FULL candy chain (base-inheriting, via BoxCandyChain) and folds
// it via MergeCandyHooks; both halves are pure (Config = spec.Config is the loader's own type,
// BoxCandyChain is an sdk mechanism), shared by the host projector and the build render.

// MergeCandyHooks concatenates PostEnable/PreRemove hook scripts across an ordered candy chain,
// one section per script kind, newline-joined in candy order. Returns nil when no candy in the
// chain declares any hook (matches the pre-split charly/hooks.go CollectHooks).
func MergeCandyHooks(candies []CandyModel) *HooksConfig {
	var postEnable, preRemove []string
	for _, c := range candies {
		if c == nil {
			continue
		}
		h := c.Hooks()
		if h == nil {
			continue
		}
		if h.PostEnable != "" {
			postEnable = append(postEnable, strings.TrimSpace(h.PostEnable))
		}
		if h.PreRemove != "" {
			preRemove = append(preRemove, strings.TrimSpace(h.PreRemove))
		}
	}
	if len(postEnable) == 0 && len(preRemove) == 0 {
		return nil
	}
	return &HooksConfig{
		PostEnable: strings.Join(postEnable, "\n"),
		PreRemove:  strings.Join(preRemove, "\n"),
	}
}

// CollectHooks collects and concatenates hooks from all candies in a box's candy chain, in candy
// order. Relocated from charly/hooks.go in the core-min wave-3 build-cluster split. The candy
// chain resolution (BoxCandyChain) and the concatenation (MergeCandyHooks) are both pure sdk.
func CollectHooks(cfg *spec.Config, layers map[string]CandyModel, boxName string) *HooksConfig {
	allCandyNames, _ := BoxCandyChain(cfg, layers, boxName)

	candies := make([]CandyModel, 0, len(allCandyNames))
	for _, name := range allCandyNames {
		if layer, ok := layers[name]; ok {
			candies = append(candies, layer)
		}
	}
	return MergeCandyHooks(candies)
}
