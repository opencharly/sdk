package loaderkit

// cross_kind_name_test.go — the sdk-seam mirror of charly's
// TestCrossKindNameReuse_LoaderAcceptsAllKinds contract (charly/charly/
// cross_kind_name_test.go): WITHIN ONE document every top-level node name is
// GLOBALLY UNIQUE. The config stack (config_stack.go) merges the raw document
// layers BEFORE the walk parses them, so the merge must reject a repeated
// top-level key inside a single layer — otherwise the collapsed document
// reaches ParseDoc and its "duplicate top-level entity name" error (parse.go)
// can never fire (the regression this test pins).
//
// Cross-FILE reuse (a box AND a candy both named `redis` in separate
// discovered documents) stays accepted — that half of charly's contract needs
// the host walk/materialize seams and lives in the charly repo.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCrossKindNameReuse_LoaderRejectsWithinDocument — the SAME top-level name
// `redis` declared twice inside ONE document (a candy node, then a local
// node) must fail LoadUnified at the config-stack merge with the duplicate-name
// contract message. The error path returns before any seam is dereferenced, so
// the empty LoadSeams is never touched.
func TestCrossKindNameReuse_LoaderRejectsWithinDocument(t *testing.T) {
	dir := t.TempDir()
	dupDoc := "version: 2026.250.0731\nredis:\n    candy:\n        base: fedora\nredis:\n    local:\n        candy: [redis]\n"
	if err := os.WriteFile(filepath.Join(dir, spec.UnifiedFileName), []byte(dupDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadUnified(dir, LoadSeams{})
	if err == nil {
		t.Fatal("LoadUnified accepted a duplicate top-level node name within one document; want the duplicate-name error")
	}
	if !strings.Contains(err.Error(), "duplicate top-level entity name") {
		t.Errorf("error = %v, want the parse-level 'duplicate top-level entity name' contract message", err)
	}
}
