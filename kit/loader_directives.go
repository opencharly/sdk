package kit

// loader_directives.go — the path-anchoring MECHANISM for discover scan specs. The kind-blind
// config-loader document DIRECTIVE TYPES (ImportEntry/ImportList, DiscoverConfig/ScanSpec) and the
// canonical manifest-filename constant moved to the types-only spec module (spec/load_directives.go,
// #55 Phase B) with the UnifiedFile they are fields of; AnchorScanSpecs stays here because it is
// mechanism (filepath resolution), retargeted onto spec.ScanSpec.

import (
	"path/filepath"

	"github.com/opencharly/spec/spec"
)

// AnchorScanSpecs returns a copy of `specs` with every relative Path
// resolved to an absolute path against `srcDir`. Absolute paths are
// kept verbatim. Empty srcDir leaves specs unchanged so the
// root-file merge (called with rootDir == workspace) is a no-op.
func AnchorScanSpecs(specs []spec.ScanSpec, srcDir string) []spec.ScanSpec {
	if srcDir == "" || len(specs) == 0 {
		return specs
	}
	out := make([]spec.ScanSpec, len(specs))
	for i, s := range specs {
		out[i] = s
		if s.Path != "" && !filepath.IsAbs(s.Path) {
			out[i].Path = filepath.Join(srcDir, s.Path)
		}
	}
	return out
}
