package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// box_build_resolve.go — pure build-side box resolvers relocated from charly core (K3 build-engine,
// Unit 2 leaf movers). Both are PURE over (cfg, layers, boxName): they read cfg.BoxConfig + the
// candy-order/direct-candy walks + per-layer accessors — no registry, loader, filesystem, or
// package-main type. They belong beside the other deploykit box collectors (CollectBoxPorts/
// Volume/Descriptions/Security/Hooks/Shell) so both charly core AND candy/plugin-build (running the
// build-engine RESOLVE plugin-side) call the ONE copy (R3).

// CollectBoxAlias gathers aliases from the box's own candies + box-level config. No base-chain
// traversal — aliases are leaf-box specific. Candy aliases come first; box-level entries override
// by name. Relocated from charly/alias_collect.go.
func CollectBoxAlias(cfg *spec.Config, layers map[string]spec.CandyReader, boxName string) ([]spec.CollectedAlias, error) {
	img, ok := cfg.BoxConfig(boxName)
	if !ok {
		return nil, fmt.Errorf("box %q not found in charly.yml", boxName)
	}

	// Resolve candies for this box (leaf-specific — aliases do NOT inherit from a base box; the
	// shared BoxDirectCandies walk).
	resolved, err := BoxDirectCandies(cfg, layers, boxName)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []spec.CollectedAlias

	// Collect from candies.
	for _, candyName := range resolved {
		layer, ok := layers[candyName]
		if !ok || !layer.HasAliases() {
			continue
		}
		for _, a := range layer.Alias() {
			if seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			result = append(result, spec.CollectedAlias(a))
		}
	}

	// Collect from box config (overrides candy aliases with the same name).
	for _, a := range img.Alias {
		cmd := a.Command
		if cmd == "" {
			cmd = a.Name
		}
		if seen[a.Name] {
			for i := range result {
				if result[i].Name == a.Name {
					result[i].Command = cmd
					break
				}
			}
		} else {
			seen[a.Name] = true
			result = append(result, spec.CollectedAlias{Name: a.Name, Command: cmd})
		}
	}

	return result, nil
}

// ResolveBoxEngine returns the run engine for a specific box: candy-level engine requirements
// (transitive closure) win over the global default. Deploy-time overrides come from
// BundleNode.Engine via ResolveBoxEngineForDeploy / ResolveBoxEngineFromMeta. Relocated from
// charly/engine.go (build-side resolve; distinct from the deploy-side ForDeploy/FromMeta twins).
func ResolveBoxEngine(cfg *spec.Config, layers map[string]spec.CandyReader, boxName string, globalRunEngine string) string {
	img, ok := cfg.BoxConfig(boxName)
	if !ok {
		return globalRunEngine
	}
	resolved, err := ResolveCandyOrder(img.Candy, layers, nil)
	if err == nil {
		for _, candyName := range resolved {
			if layer, ok := layers[candyName]; ok && layer.Engine() != "" {
				return layer.Engine()
			}
		}
	}
	return globalRunEngine
}
