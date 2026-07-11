package kit

import (
	"strconv"
	"strings"
)

// CompareCalVer compares two CalVer strings numerically component-by-component,
// falling back to lexical comparison for any non-numeric component. Returns -1
// if a < b, +1 if a > b, 0 if equal. Relocated from charly core (P8) so the build
// render engine (sdk/deploykit) and charly's image-tag logic share ONE comparator.
// Distinct from CalVer.Less, which requires strictly-canonical parsed CalVers; this
// is the lenient dotted-string comparator the build/tag paths use.
func CompareCalVer(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := len(aParts)
	if len(bParts) < n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		ai, aErr := strconv.Atoi(aParts[i])
		bi, bErr := strconv.Atoi(bParts[i])
		if aErr != nil || bErr != nil {
			// Fall back to lexical for this component.
			if aParts[i] < bParts[i] {
				return -1
			}
			if aParts[i] > bParts[i] {
				return 1
			}
			continue
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	if len(aParts) < len(bParts) {
		return -1
	}
	if len(aParts) > len(bParts) {
		return 1
	}
	return 0
}
