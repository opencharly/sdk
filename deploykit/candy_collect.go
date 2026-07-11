package deploykit

import "github.com/opencharly/sdk/buildkit"

// CollectAllBoxCandies returns an image's complete candy set (its own resolved
// candies plus every candy inherited through the full base chain), in first-seen
// order. Relocated from charly core (P8); byte-identical to the former
// charly/intermediates.go collectAllBoxCandies.
//
// walked is an IMAGE-visited guard for the base-chain recursion: a base edge may
// form a cycle (A.base=B, B.base=A) — caught + reported by ResolveBoxOrder, but
// without this guard the walk recurses a cyclic chain until the stack overflows.
// Re-visiting a base also can't add new candies, so skipping it is correct for the
// acyclic case too.
func CollectAllBoxCandies(boxName string, boxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel) []string {
	seen := make(map[string]bool)
	walked := make(map[string]bool)
	var result []string

	var walk func(name string)
	walk = func(name string) {
		if walked[name] {
			return
		}
		walked[name] = true
		img, ok := boxes[name]
		if !ok {
			return
		}
		if !img.IsExternalBase {
			walk(img.Base)
		}
		resolved, err := ResolveCandyOrder(img.Candy, layers, nil)
		if err != nil {
			return
		}
		for _, l := range resolved {
			if !seen[l] {
				seen[l] = true
				result = append(result, l)
			}
		}
	}
	walk(boxName)
	return result
}
