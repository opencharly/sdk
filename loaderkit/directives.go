// directives.go — the loaderkit-local WALK-only directives: MaxIncludeDepth, namespaceAliasRe, and
// validateNamespaceAlias. The kind-blind document directives (ImportList/ImportEntry,
// DiscoverConfig/ScanSpec, anchorScanSpecs, docShape/classifyDoc) relocated to sdk/kit
// (loader_directives.go + loader_classify.go) so charly core AND sdk/loaderkit share ONE copy (R3) —
// see kit.ImportList / kit.DiscoverConfig / kit.AnchorScanSpecs / kit.ClassifyDoc.
package loaderkit

import (
	"fmt"
	"regexp"
)

// MaxIncludeDepth caps recursive include resolution. A cycle or excessive depth raises a clear
// error with the offending file path.
const MaxIncludeDepth = 8

// namespaceAliasRe constrains an `import:` namespace alias to a bare lowercase-hyphenated
// identifier — no dots, since `.` is the qualified-reference separator (`alias.entry`).
var namespaceAliasRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validateNamespaceAlias enforces a bare lowercase-hyphenated alias (no dots).
func validateNamespaceAlias(alias string) error {
	if !namespaceAliasRe.MatchString(alias) {
		return fmt.Errorf("import namespace alias %q must match %s", alias, namespaceAliasRe.String())
	}
	return nil
}
