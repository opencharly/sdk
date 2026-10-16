package candywalk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// candywalk_refs_template_test.go — prose about refs is not a ref.
//
// The notation the org's docs use to DESCRIBE a ref is `@github.com/opencharly/<kind>-<name>:<tag>`.
// Before this fix that token matched `remoteRefRe` like any other, parsed as a repo path plus a
// version, and reached the resolver — which ran `git ls-remote` against
// `https://github.com/opencharly/<kind>-<name>.git <tag>` and got a GitHub 400. Measured effect:
// `charly marketplace generate|drift` died in `collect entities` before reading a single entity,
// because the marketplace checkout's own CHANGELOG carries that shape in prose.
//
// The real refs below are SINGLE-QUOTED, as in the sibling fixture: `'` is one of the delimiters the
// ref class stops at, so the token comes back bare. (Writing them in backticks — which the class does
// NOT exclude — makes the collected token carry a trailing backtick, and an equality assertion against
// the bare ref then fails for a reason that has nothing to do with refs.)
func TestCollectRefs_IgnoresProseTemplates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CHANGELOG.md")
	const body = "# CHANGELOG\n\n" +
		"A ref is written `@github.com/opencharly/<kind>-<name>:<tag>` — a placeholder, not a dependency.\n\n" +
		"- '@github.com/opencharly/layer-nodejs:v2026.240.0001'\n" +
		"- '@github.com/opencharly/pod-charly-hooks:feat/kubevirt-marketplace-family-2'\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)

	// The property this change adds: no prose placeholder may ever be returned as a ref.
	for _, ref := range got {
		if strings.ContainsAny(ref, "<>") {
			t.Errorf("collectRefs returned the prose template %q — it is fetched as if it were a repository", ref)
		}
	}
	// And the real forms on the surrounding lines must still come through, including the BRANCH ref —
	// the whole reason the `:v…` anchor was dropped from this pattern in the first place.
	for _, want := range []string{
		"@github.com/opencharly/layer-nodejs:v2026.240.0001",
		"@github.com/opencharly/pod-charly-hooks:feat/kubevirt-marketplace-family-2",
	} {
		found := false
		for _, ref := range got {
			if ref == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("collectRefs DROPPED the real ref %q — got %v", want, got)
		}
	}
}
