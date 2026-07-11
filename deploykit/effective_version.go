package deploykit

import (
	"fmt"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// ComputeEffectiveVersions assigns ResolvedBox.EffectiveVersion for every image in
// the build graph. EffectiveVersion is the content-derived identity emitted as the
// ai.opencharly.version OCI label. Relocated from charly core (P8); byte-identical.
//
//  1. the image's dedicated `version:` (img.Version) if set; else
//  2. the highest candy `version:` across its full candy set (CollectAllBoxCandies
//     spans the entire base chain); else
//  3. the internal base image's EffectiveVersion (recurse); else
//  4. a HARD ERROR — there is NO build-timestamp fallback.
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

		// 1. A dedicated version: wins.
		if img.Version != "" {
			memo[name] = img.Version
			return img.Version, nil
		}

		// 2. Highest candy version across the full candy set (own + base chain).
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

		// 3. Candy-free internal-base image inherits the base's effective version.
		if !img.IsExternalBase && img.Base != "" {
			bv, err := compute(img.Base)
			if err != nil {
				return "", err
			}
			memo[name] = bv
			return bv, nil
		}

		// 4. Nothing derivable — hard cutover: no build-timestamp fallback.
		return "", fmt.Errorf("image %q resolves no version: a candy-free image on an external base needs a dedicated `version:` field", name)
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
