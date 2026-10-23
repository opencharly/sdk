package candywalk

import (
	"os"
	"path/filepath"
	"testing"
)

// candywalk_refs_declared_test.go — the DECLARED/contained split (opencharly/sdk#455).
//
// A ref a manifest DECLARES is a scalar in it; a ref it merely CONTAINS is data — a `command:` body
// that writes fixtures at runtime, a folded description, a generated-manifest template. The raw-byte
// scan could not tell them apart, so `plugin-box`'s R10 bed — which writes a deliberately
// unfetchable ref into a scratch project to prove `charly box reconcile --closure` REPORTS it —
// collected its own fixture and killed the whole corpus walk resolving it. The bed is legitimate and
// must keep its realistic ref, so the fix is the instrument: the parser knows which scalars are
// declarations.

// TestCollectRefs_IgnoresRefsInsideBlockScalars is the R7 gate on that fix: the fixture's ref must
// not surface, and the ref DECLARED beside it in the same file must not be lost.
func TestCollectRefs_IgnoresRefsInsideBlockScalars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, UnifiedFileName)
	// Shaped exactly like plugin-box's real box-reconcile-closure-reports-unfetchable step.
	const body = `plugin-box:
    candy:
        require:
            - '@github.com/opencharly/plugin-box-libs:v2026.240.0001'
        description: >-
            A folded body naming @github.com/opencharly/folded-fixture:v2026.240.0002 is prose,
            not a declaration, and must not be collected.
        plan:
            - check: the closure report NAMES an unfetchable ref
              id: box-reconcile-closure-reports-unfetchable
              context:
                  - runtime
              command:
                command: |-
                  set -eu
                  D=$(mktemp -d)
                  mkdir -p "$D/candy/x"
                  cat > "$D/candy/x/charly.yml" <<'CANDY'
                  x:
                      candy:
                          require:
                              - '@github.com/opencharly/this-repo-does-not-exist-zzz/candy/nope:v1.2.3'
                  CANDY
              in_container: false
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)
	want := "@github.com/opencharly/plugin-box-libs:v2026.240.0001"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("collectRefs = %v, want exactly [%q] — a ref inside a block scalar is DATA "+
			"(the plugin-box fixture must not reach the resolver) and a declared ref must never "+
			"vanish (opencharly/sdk#455 / #322)", got, want)
	}
}

// TestCollectRefs_StillCollectsAGenuinelyMalformedRef is the NEGATIVE arm: the same ref string that
// must be ignored inside a block scalar must still be COLLECTED when it is genuinely declared, so
// the scanner is not simply blinded. A real malformed root ref has to reach the resolver and fail
// loudly there (that is `generate` rc=1 naming it), which is exactly what the fixture's bed asserts
// about the closure report.
func TestCollectRefs_StillCollectsAGenuinelyMalformedRef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, UnifiedFileName)
	const body = `root:
    candy:
        require:
            - '@github.com/opencharly/this-repo-does-not-exist-zzz/candy/nope:v1.2.3'
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)
	want := "@github.com/opencharly/this-repo-does-not-exist-zzz/candy/nope:v1.2.3"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("collectRefs = %v, want exactly [%q] — a DECLARED malformed ref must reach the "+
			"resolver and fail there; dropping it would hide a broken refs list (opencharly/sdk#455)",
			got, want)
	}
}

// TestCollectRefs_ReadsDeclarationsNotComments pins the second consequence of parsing: a versioned
// ref written in a COMMENT declares nothing and is no longer fetched, while the same token in a
// declaration is. The org-wide measurement behind this row: the only comment-only ref in the 596
// in-repo manifests is `plugin-quickshell` documenting its own pin, which is a DIRECT pin already.
func TestCollectRefs_ReadsDeclarationsNotComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, UnifiedFileName)
	const body = `# A consumer pins
# '@github.com/opencharly/commented-not-declared:v2026.240.0003' — this is a comment.
declared:
    candy:
        require:
            - '@github.com/opencharly/really-declared:v2026.240.0004'
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)
	want := "@github.com/opencharly/really-declared:v2026.240.0004"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("collectRefs = %v, want exactly [%q] — a comment declares nothing, a scalar does",
			got, want)
	}
}
