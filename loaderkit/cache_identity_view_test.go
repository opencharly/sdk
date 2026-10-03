package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/cache"
)

// TestReshapeViewIdentity_UnifiedWithStoreKey — the derived-view identity and the
// materialized-tree Store key MUST derive from the SAME (schema, loader) components (R3:
// one canonical computation). This test pins reshapeViewIdentityFn to
// cache.KeyDigest(schemaLoaderComponents())[:16] and asserts it re-keys when the module
// identities change — i.e. it fails if the view identity is re-implemented independently
// of schemaLoaderComponents (the duplication this unifies), and it fails if the view path
// stops re-keying on a schema/loader change.
func TestReshapeViewIdentity_UnifiedWithStoreKey(t *testing.T) {
	orig := moduleIdentityFn
	defer func() { moduleIdentityFn = orig }()

	moduleIdentityFn = func(p string) string { return p + "@v1" }
	got := reshapeViewIdentity()
	want := cache.KeyDigest(schemaLoaderComponents())[:16]
	if got != want {
		t.Fatalf("view identity = %q, want the shared-components digest %q — the view path "+
			"must derive from schemaLoaderComponents, not its own algorithm (R3)", got, want)
	}
	if len(got) != 16 {
		t.Fatalf("view identity must be a 16-hex-char prefix, got %q", got)
	}

	// Change the schema identity only → the view identity MUST change.
	moduleIdentityFn = func(p string) string { return p + "@v2" }
	if reshapeViewIdentity() == got {
		t.Fatal("view identity must re-key when a schema/loader identity changes")
	}
}
