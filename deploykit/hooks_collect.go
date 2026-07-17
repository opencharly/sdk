package deploykit

import "strings"

// hooks_collect.go — the pure candy-hooks merge logic (W9: the CollectHooks split, same
// rationale as MergeCandySecurity in security.go). charly's CollectHooks (hooks.go) stays the
// host-side wrapper that resolves the box's FULL candy chain (base-inheriting — a *Config/
// walkBaseChain concern, genuinely core) and calls this pure fold over the CandyModel interface.

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
