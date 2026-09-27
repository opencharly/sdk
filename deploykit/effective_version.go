package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// ComputeEffectiveVersions assigns ResolvedBox.EffectiveVersion for every image in
// the build graph. EffectiveVersion is the content-derived identity emitted as the
// ai.opencharly.version OCI label. Relocated from charly core (P8); reworked by the
// schema-versioning removal cutover: the authored box/candy `version:` source is GONE,
// so the one REAL source is the candy chain's git tags (the resolved candy version IS
// the CalVer git tag it was fetched at).
//
//  1. the highest source candy git tag across its full candy set (CollectAllBoxCandies
//     spans the entire base chain); else
//  2. the internal base image's EffectiveVersion (recurse); else
//  3. EMPTY — no fabricated version. An image composed only of local (in-tree) candies
//     has no source git tag to derive from; it carries an empty label rather than a
//     build-timestamp version (the "no fabricated version" cutover rule).
//
// Run AFTER ComputeIntermediates + GlobalCandyOrder so the base chain and the
// auto-intermediate images are fully materialized in boxes.
func ComputeEffectiveVersions(boxes map[string]*buildkit.ResolvedBox, candies map[string]CandyModel) error {
	memo := make(map[string]string)
	visiting := make(map[string]bool)

	var compute func(name string) (string, error)
	compute = func(name string) (string, error) {
		if v, ok := memo[name]; ok {
			return v, nil
		}
		img, ok := boxes[name]
		if !ok {
			return "", fmt.Errorf("effective version: unknown image %q", name)
		}
		if visiting[name] {
			return "", fmt.Errorf("effective version: cyclic base chain at image %q", name)
		}
		visiting[name] = true
		defer delete(visiting, name)

		// 1. Highest source candy git tag across the full candy set (own + base chain).
		best := ""
		for _, ln := range CollectAllBoxCandies(name, boxes, candies) {
			l, ok := candies[ln]
			if !ok {
				continue
			}
			lv := l.GetVersion()
			if lv == "" {
				continue
			}
			if best == "" || kit.CompareCalVer(lv, best) > 0 {
				best = lv
			}
		}
		if best != "" {
			memo[name] = best
			return best, nil
		}

		// 2. Candy-free / local-only internal-base image inherits the base's effective version.
		if !img.IsExternalBase && img.Base != "" {
			bv, err := compute(img.Base)
			if err != nil {
				return "", err
			}
			memo[name] = bv
			return bv, nil
		}

		// 3. Nothing derivable — empty (no fabricated version).
		memo[name] = ""
		return "", nil
	}

	for name, img := range boxes {
		v, err := compute(name)
		if err != nil {
			return err
		}
		img.EffectiveVersion = v
	}
	return nil
}
