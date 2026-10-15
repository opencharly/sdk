package candywalk

import (
	"os"
	"path/filepath"
	"testing"
)

// candywalk_refs_class_test.go — the placeholder CLASS, not one face of it.
//
// The first fix excluded `<` and `>`, the second imposed a two-character minimum, and each was derived
// from a single observed instance. This table is the enumeration that should have come first: every
// notation the corpus actually carries, checked in BOTH directions — the prose ones must yield no ref,
// and the real ones beside them must survive. That second half is what keeps opencharly/sdk#322 fixed:
// a fail-closed pattern once dropped a real branch with no error.
func TestAcceptableVersion_RejectsEveryProseFormAndKeepsEveryRealOne(t *testing.T) {
	reject := []string{
		"v2026.xxx.yyyy", // `layer-android-sdk:v2026.xxx.yyyy` — claims the tag namespace, is not CalVer
		"vTAG",           // `charly:vTAG` — four characters, so NO length constant can reach it
		"v",              // the truncated `…/candy/omarchy-base:v` form
		"2026.xxx.yyyy",  // the v-less form of the SAME placeholder: it claims CalVer shape, so it must BE CalVer
		"vx.y.z",         // opencharly/sdk#412: every component one or two characters — placeholder-shaped,
		// not a ref. Context cannot decide this (a backticked REAL ref must stay accepted,
		// which is why "inside a code span means prose" was rejected), so the shape does.
	}
	// A REAL ref written inside markdown backticks must still be collected — and collected CLEAN, with
	// the delimiter removed by the scanner rather than left for the validator to reject. This is the
	// sdk#322 failure mode in a new coat: the fixture for the FIRST fix carried backticks and the
	// captured token kept one, which is why the delimiter set now names the backtick beside ' and ".
	backticked := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(backticked, []byte("Pin `@github.com/opencharly/layer-ripgrep:v2026.240.0001` here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bt := collectRefs(backticked)
	if len(bt) != 1 || bt[0] != "@github.com/opencharly/layer-ripgrep:v2026.240.0001" {
		t.Errorf("a backticked real ref must be collected clean; got %v", bt)
	}

	accept := []string{
		"v2026.288.1718",     // CalVer
		"v0.2026288.1718",    // the Go-module CalVer
		"v1.2.3",             // semver
		"v2026.288.1254-rc1", // semver pre-release
		"main",               // a branch
		"latest",             //
		"v2-feature",         // a branch that STARTS v<digit> — the review round's finding:
		// version-shaped tokens that are REAL and must keep resolving — a review round
		// rejected the first, over-broad form of this rule, which is why these are rows:
		"1.2.3", // semver without the v
		// (no row for `a.b.c`: letters-only components make it indistinguishable from the stub by
		//  design — that is the accepted cost of this rule, recorded here rather than hidden.)
		// rejecting this is sdk#322, and it is now a fixture row rather than a sentence
		// in a PR body.
		"feat/kubevirt-marketplace-family-2", // a branch with a slash — the sdk#322 case
		"a0401d7",                            // a commit SHA
		"2026.108.56",                        // a plain CalVer tag
	}
	for _, v := range reject {
		if acceptableVersion(v) {
			t.Errorf("acceptableVersion(%q) = true — a prose placeholder would reach the resolver (sdk#410)", v)
		}
	}
	for _, v := range accept {
		if !acceptableVersion(v) {
			t.Errorf("acceptableVersion(%q) = false — a REAL ref was dropped, which is the sdk#322 defect", v)
		}
	}
}

// And the same class through the whole scan, so the wiring is proven and not just the predicate.
func TestCollectRefs_DropsTheProseFormsAndKeepsTheRealRefsBesideThem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const body = "A ref looks like `@github.com/opencharly/<kind>-<name>:<tag>`, or " +
		"`@github.com/opencharly/layer-android-sdk:v2026.xxx.yyyy`, or `@github.com/opencharly/charly:vTAG`.\n\n" +
		"A real one: '@github.com/opencharly/layer-nodejs:v2026.240.0001'.\n" +
		"A real branch: '@github.com/opencharly/pod-charly-hooks:feat/kubevirt-marketplace-family-2'.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := collectRefs(path)
	want := []string{
		"@github.com/opencharly/layer-nodejs:v2026.240.0001",
		"@github.com/opencharly/pod-charly-hooks:feat/kubevirt-marketplace-family-2",
	}
	if len(got) != len(want) {
		t.Fatalf("collectRefs returned %d refs, want %d — got %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("collectRefs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
