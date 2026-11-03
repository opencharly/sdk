package candywalk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// candywalk_refs_truncation_test.go — the sibling shape that survived the first fix.
//
// `opencharly/sdk#405` fixed the notation with the placeholder in the REPO NAME
// (`@github.com/opencharly/<kind>-<name>:<tag>`) by excluding `<` and `>`. That fix was correct and
// incomplete: in this second shape the placeholder sits in the VERSION and the repo path is real, so
// the match stops just before the `<` and survives with a one-character version `v` — which reaches
// the resolver as a ref that cannot exist (`could not resolve ref "v"`, measured — `opencharly/sdk#408`).
//
// The rule this pins: every real version is at least two characters — a CalVer tag, a branch, a SHA —
// so requiring two rejects the stub without narrowing any legitimate form. The real ref beside it must
// still come through, which is the half of this test that would catch an over-tightened pattern.
func TestCollectRefs_IgnoresATruncatedPlaceholderVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CHANGELOG.md")
	const body = "A ref looks like `@github.com/opencharly/layer-omarchy-base/candy/omarchy-base:v<version>`.\n\n" +
		"- '@github.com/opencharly/layer-nodejs:v2026.240.0001'\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)

	// No truncated placeholder may be returned as a ref.
	for _, ref := range got {
		if strings.HasSuffix(ref, ":v") {
			t.Errorf("collectRefs returned %q — a truncated placeholder version reached the resolver "+
				"(a one-character version is not a version: opencharly/sdk#408)", ref)
		}
		if strings.ContainsAny(ref, "<>") {
			t.Errorf("collectRefs returned %q — a placeholder survived exclusion", ref)
		}
	}
	// And the real ref on the line beside it must still resolve, or the pattern was over-tightened.
	want := "@github.com/opencharly/layer-nodejs:v2026.240.0001"
	found := false
	for _, ref := range got {
		if ref == want {
			found = true
		}
	}
	if !found {
		t.Errorf("collectRefs DROPPED the real ref %q — got %v", want, got)
	}
}
