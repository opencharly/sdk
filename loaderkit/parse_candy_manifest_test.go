package loaderkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// parse_candy_manifest_test.go — the stacked-manifest parse (the plugins->candies migration):
// a candy manifest may stack a `candy:` node plus sibling kind entities (`skill:`/`hook:`/
// `marketplace:`); ParseCandyManifest (the discovered-candy scan) must find the candy node among
// them and return ITS body — the sibling entities are resolved by the full load path (ParseDoc).

func TestParseCandyManifest_StackedSkillNode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "charly.yml")
	body := `postgresql:
    candy:
        version: 2026.218.1200
        description: Postgres 16 + contrib.
        plan:
            - check: /usr/bin/postgres exists
postgresql-skill:
    skill:
        name: postgresql
        family: charly-infrastructure
        owner: postgresql
        description: Use when working with postgresql.
        content: |
            # Postgresql
            body
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th := spec.Threaded{Kinds: map[string]bool{
		"candy": true, "skill": true, "hook": true, "marketplace": true,
	}}
	got, err := ParseCandyManifest(path, th, spec.CandyVocab{})
	if err != nil {
		t.Fatalf("ParseCandyManifest must accept a candy node stacked with a skill node: %v", err)
	}
	if got.Name != "postgresql" {
		t.Fatalf("candy name = %q, want postgresql (the node key, not the skill entity)", got.Name)
	}
	if got.Description != "Postgres 16 + contrib." || len(got.Plan) != 1 {
		t.Fatalf("candy body not decoded from the stacked manifest: %+v", got)
	}
}

// TestParseCandyManifest_MarketplaceOnlyRejected proves a file with NO candy node (a marketplace
// entity alone) is NOT a candy manifest — ParseCandyManifest rejects it, so a marketplace-only
// file must not live in the candy scan path.
func TestParseCandyManifest_MarketplaceOnlyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "charly.yml")
	body := `marketplace:
    marketplace:
        name: charly-plugins
        version: 3.2.0
        families: {}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	th := spec.Threaded{Kinds: map[string]bool{"marketplace": true}}
	if _, err := ParseCandyManifest(path, th, spec.CandyVocab{}); err == nil {
		t.Fatal("ParseCandyManifest must reject a marketplace-only file (no candy node)")
	}
}
