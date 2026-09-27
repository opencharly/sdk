package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestLoadedProjectCacheKey_ReKeysOnIdentityChange — the materialized-tree cache key carries
// the schema (spec module) and loader (sdk module) identities, so a change to either re-keys
// the entry. Fails if the key stops re-keying on an identity change (i.e. if the identity
// components are dropped or constant). Replaces the deleted
// TestMaterializedCache_LoaderIdentityDriftReKeys coverage.
func TestLoadedProjectCacheKey_ReKeysOnIdentityChange(t *testing.T) {
	orig := moduleIdentityFn
	defer func() { moduleIdentityFn = orig }()

	moduleIdentityFn = func(p string) string { return p + "@v1" }
	k1, c1, err := loadedProjectCacheKey(&spec.LoadedProject{})
	if err != nil {
		t.Fatalf("loadedProjectCacheKey: %v", err)
	}
	if c1["schema_identity"] == "" || c1["loader_identity"] == "" {
		t.Fatalf("cache key must carry schema_identity + loader_identity, got %v", c1)
	}
	if c1["schema_identity"] != specModulePath+"@v1" || c1["loader_identity"] != sdkModulePath+"@v1" {
		t.Fatalf("identity components not wired to the module identities: %v", c1)
	}

	// Change the loader identity only → the key MUST change.
	moduleIdentityFn = func(p string) string { return p + "@v2" }
	k2, _, err := loadedProjectCacheKey(&spec.LoadedProject{})
	if err != nil {
		t.Fatalf("loadedProjectCacheKey: %v", err)
	}
	if k1 == k2 {
		t.Fatal("cache key must re-key when a schema/loader identity changes")
	}
}
