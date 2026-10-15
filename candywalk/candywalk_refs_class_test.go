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

	// KNOWN RESIDUAL, deliberately not "fixed": `vx.y.z` is accepted, because every rule that rejects
	// it also rejects real branches — `v2-feature`, or `vue-migration` under a broad `^v[A-Za-z]` —
	// and dropping a real mutable ref silently is the opencharly/sdk#322 defect this repo already
	// paid for. `vx.y.z` appears nowhere in the scanned corpus; the placeholder forms that DO appear
	// are all in the reject list below. A residual that is written down is a decision; one that is
	// discovered later is a bug.
	accept := []string{
		"v2026.288.1718",                     // CalVer
		"v0.2026288.1718",                    // the Go-module CalVer
		"v1.2.3",                             // semver
		"v2026.288.1254-rc1",                 // semver pre-release
		"main",                               // a branch
		"latest",                             //
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
