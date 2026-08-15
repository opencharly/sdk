package deploykit

import (
	"fmt"

	"github.com/opencharly/spec/spec"
)

// config_candy_chain.go — the candy-chain walkers relocated from charly/candy_chain.go
// (FLOOR-SLIM Unit 5). Their real dependency is ResolveCandyOrder, whose implementation lives
// HERE in deploykit (charly's own copy is a 1-line wrapper, per graph_shim.go) — since deploykit
// is the top of the sdk dependency graph (it already imports both spec and buildkit), this is the
// only cycle-free home; spec and buildkit cannot import deploykit. Free functions taking
// *spec.Config as their first parameter (Config-as-method is impossible here for the same reason:
// a type's methods must live in its own defining package).

// BoxOwner resolves a (possibly namespace-qualified) box ref to the Config that OWNS it, the leaf
// name inside that owner, and its authored BoxConfig. It is the ONE namespace-aware entry every
// box collector in this package shares.
//
// Why it exists: after the box inversion every box the superproject builds is reached under an
// import namespace (`fedora.charly-fedora`), while `cfg.BoxConfig` / `cfg.WalkBaseChain` are
// deliberately ROOT-INTERNAL flat lookups. Reading a qualified name through them found nothing, so
// every collector keyed on the box's own authored config silently produced an EMPTY result —
// no `ai.opencharly.description` (ADE's baked plan), no `port` (and therefore no EXPOSE), no
// `volume`, `security`, `shell`, `alias` or `hook` on the entire imported catalog, with
// `charly check box` reporting "No plan steps defined for this image" and exiting green.
//
// The descent mirrors buildkit.ResolveBox's own entry descent (config_resolve.go): resolution of
// WHICH config owns a name is a NAME-resolution concern that spec.ResolveBoxRef already owns, so
// every collector becomes namespace-aware through this single chokepoint instead of each
// re-implementing (or, as before, omitting) it. spec.WalkBaseChain keeps its documented
// root-internal contract unchanged — it is walked in the OWNER's config, where a base ref is
// relative, and its deliberate refusal to cross a namespace boundary MID-chain still holds.
//
// The candy map stays flat and shared across namespaces (its keys are bare local names or full
// remote refs, never namespace-qualified), which is why the owner's namespace-relative candy refs
// resolve against the SAME layers map the render path already uses.
func BoxOwner(cfg *spec.Config, boxName string) (*spec.Config, string, spec.BoxConfig, bool) {
	img, owner, ok := cfg.ResolveBoxRef(boxName)
	if !ok {
		return nil, "", spec.BoxConfig{}, false
	}
	return owner, spec.LeafName(boxName), img, true
}

// BoxCandyChain returns the ordered, de-duplicated candy map-keys for boxName across its FULL
// base-image chain (box → base → base's base), candy-order per level. This is the ONE walk every
// BASE-CHAIN field collector shares (CollectHooks, CollectShell, CollectDescriptions,
// CollectBoxVolume, CollectBoxPorts) — so a contribution a base box makes (a volume, a check
// check, a published port) is inherited by every box built on it. De-duplication is
// first-occurrence-wins by candy key.
//
// boxName may be namespace-qualified; the chain is then walked inside the owning namespace's
// Config (BoxOwner), where the base refs it follows are relative.
//
// On a ResolveCandyOrder error at a level the walk stops there, returning what was collected so
// far PLUS the error — callers that propagate it keep doing so; callers that swallow it and use
// the partial result keep doing that by ignoring the returned error.
func BoxCandyChain(cfg *spec.Config, layers map[string]CandyModel, boxName string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	owner, leaf, _, ok := BoxOwner(cfg, boxName)
	if !ok {
		return nil, nil
	}
	for _, node := range owner.WalkBaseChain(leaf) {
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
// boxName may be namespace-qualified (BoxOwner).
func BoxDirectCandies(cfg *spec.Config, layers map[string]CandyModel, boxName string) ([]string, error) {
	_, _, img, ok := BoxOwner(cfg, boxName)
	if !ok {
		return nil, fmt.Errorf("box %q not found in charly.yml", boxName)
	}
	return ResolveCandyOrder(img.Candy, layers, nil)
}
