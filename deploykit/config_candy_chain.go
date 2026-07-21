package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// config_candy_chain.go — the candy-chain walkers relocated from charly/candy_chain.go
// (FLOOR-SLIM Unit 5). Their real dependency is ResolveCandyOrder, whose implementation lives
// HERE in deploykit (charly's own copy is a 1-line wrapper, per graph_shim.go) — since deploykit
// is the top of the sdk dependency graph (it already imports both spec and buildkit), this is the
// only cycle-free home; spec and buildkit cannot import deploykit. Free functions taking
// *spec.Config as their first parameter (Config-as-method is impossible here for the same reason:
// a type's methods must live in its own defining package).

// BoxCandyChain returns the ordered, de-duplicated candy map-keys for boxName across its FULL
// base-image chain (box → base → base's base), candy-order per level. This is the ONE walk every
// BASE-CHAIN field collector shares (CollectHooks, CollectShell, CollectDescriptions,
// CollectBoxVolume, CollectBoxPorts) — so a contribution a base box makes (a volume, a check
// check, a published port) is inherited by every box built on it. De-duplication is
// first-occurrence-wins by candy key.
//
// On a ResolveCandyOrder error at a level the walk stops there, returning what was collected so
// far PLUS the error — callers that propagate it keep doing so; callers that swallow it and use
// the partial result keep doing that by ignoring the returned error.
func BoxCandyChain(cfg *spec.Config, layers map[string]CandyModel, boxName string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, node := range cfg.WalkBaseChain(boxName) {
		resolved, err := ResolveCandyOrder(node.Img.Candy, layers, nil)
		if err != nil {
			return out, err
		}
		for _, name := range resolved {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// BoxDirectCandies returns the ordered, transitively-resolved candy map-keys for boxName's OWN
// candies only — NO base-chain traversal. The shared walk for LEAF-SPECIFIC fields
// (CollectSecurity, CollectBoxAlias) that intentionally do NOT inherit from a base box.
func BoxDirectCandies(cfg *spec.Config, layers map[string]CandyModel, boxName string) ([]string, error) {
	img, ok := cfg.BoxConfig(boxName)
	if !ok {
		return nil, fmt.Errorf("box %q not found in charly.yml", boxName)
	}
	return ResolveCandyOrder(img.Candy, layers, nil)
}
