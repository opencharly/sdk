package candywalk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectRefs_AcceptsEveryRefForm is the R7 gate on opencharly/sdk#322: the ref collector must
// hand over EVERY ref form the loader can resolve. It also pins the one deliberate skip — the
// pre-cutover in-repo charly form — and pins that a VERSIONLESS prose mention is not collected,
// which is why the issue's "hard-error" arm is not a viable text-level check (see collectRefs).
//
// The former pattern (`:v[0-9]+\.[0-9]+\.[0-9]+`) matched a CalVer tag and nothing else, so a
// branch (`:feat/…`) or a commit SHA matched NOTHING — no error, no warning. In a fail-closed
// generator that means the entity under test is never read, the generator validates a STALE one,
// and it reports a failure the author cannot fix by editing the map, because the map is never
// read. Pre-fix this test sees one ref where three were declared.
func TestCollectRefs_AcceptsEveryRefForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, UnifiedFileName)
	body := `x:
    candy:
        require:
            - '@github.com/opencharly/layer-nodejs:v2026.240.0001'
            - '@github.com/opencharly/pod-charly-hooks:feat/kubevirt-marketplace-family-2'
            - '@github.com/opencharly/plugin-kube:1a7935d4b6c1e0f2a3b4c5d6e7f8091a2b3c4d5e'
            - '@github.com/opencharly/charly/candy/charly-core'
        plan:
            - check: fixture
              id: fixture-ok
              context:
                  - build
              command: "true"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)
	want := map[string]bool{
		"@github.com/opencharly/layer-nodejs:v2026.240.0001":                          false,
		"@github.com/opencharly/pod-charly-hooks:feat/kubevirt-marketplace-family-2":  false,
		"@github.com/opencharly/plugin-kube:1a7935d4b6c1e0f2a3b4c5d6e7f8091a2b3c4d5e": false,
	}
	for _, ref := range got {
		if _, ok := want[ref]; !ok {
			t.Fatalf("collectRefs returned an unexpected ref %q (the in-repo charly form must be skipped)", ref)
		}
		want[ref] = true
	}
	for ref, seen := range want {
		if !seen {
			t.Fatalf("collectRefs DROPPED %q — a declared ref must never vanish silently "+
				"(opencharly/sdk#322); got %v", ref, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("collectRefs got %d refs, want %d: %v", len(got), len(want), got)
	}
}
