package deploykit

import (
	"github.com/opencharly/spec/spec"
)

// shell_collect.go — the box SHELL-INIT aggregator (relocated from charly/shellcollect.go in the
// core-min wave-3 build-cluster split). CollectShell is a PURE candy-chain walk (spec.Config +
// layers, via BoxCandyChain) producing the three-section spec.LabelShellSet, beside its sibling
// chain-collectors (CollectDescriptions, MergeCandyHooks). Mirrors CollectDescriptions shape:
// dedupe by candy name, walk the composed candies, terminate on visited-image cycle.
//
// Section assignment:
//   - Each candy's `shell:` (intrinsic + per-shell sub-blocks) -> Candy.
//   - Box-level `shell:` -> Box.
//   - Deploy is never populated: the deploy-scope `shell:` overlay authoring field was retired
//     outright; the LabelShellSet.Deploy wire section stays (a stable three-section label shape)
//     but permanently empty until a properly-designed feature populates it.
//
// Returns nil if every section is empty.
func CollectShell(cfg *spec.Config, layers map[string]CandyModel, boxName string) *spec.LabelShellSet {
	set := &spec.LabelShellSet{}

	allCandyNames, _ := BoxCandyChain(cfg, layers, boxName)
	for _, candyName := range allCandyNames {
		layer, ok := layers[candyName]
		if !ok {
			continue
		}
		entry := shellConfigToEntry(layer.Shell(), candyName)
		if entry == nil {
			continue
		}
		entry.ID = candyName
		set.Candy = append(set.Candy, *entry)
	}

	if _, _, img, ok := BoxOwner(cfg, boxName); ok {
		if img.Shell != nil {
			entry := shellConfigToEntry(img.Shell, "box:"+boxName)
			if entry != nil {
				entry.ID = "box:" + boxName
				set.Box = append(set.Box, *entry)
			}
		}
	}

	if len(set.Candy) == 0 && len(set.Box) == 0 && len(set.Deploy) == 0 {
		return nil
	}
	return set
}

// shellConfigToEntry projects an in-memory spec.Shell into the label-emission spec.ShellEntry
// shape. Returns nil when the config is effectively empty (no Init, no PathAppend, no per-shell
// overrides).
func shellConfigToEntry(cfg *spec.Shell, origin string) *spec.ShellEntry {
	if cfg == nil {
		return nil
	}
	hasGeneric := cfg.Init != "" || len(cfg.PathAppend) > 0 || cfg.Path != ""
	if !hasGeneric && len(cfg.ByShell()) == 0 {
		return nil
	}
	entry := &spec.ShellEntry{
		Origin:   origin,
		Priority: cfg.Priority,
	}
	if hasGeneric {
		entry.Generic = &ShellSpec{
			Init:       cfg.Init,
			PathAppend: append([]string(nil), cfg.PathAppend...),
			Path:       cfg.Path,
		}
	}
	if len(cfg.ByShell()) > 0 {
		entry.ByShell = make(map[string]*ShellSpec, len(cfg.ByShell()))
		for k, v := range cfg.ByShell() {
			if v == nil {
				continue
			}
			entry.ByShell[k] = &ShellSpec{
				Init:       v.Init,
				PathAppend: append([]string(nil), v.PathAppend...),
				Path:       v.Path,
			}
		}
	}
	return entry
}
