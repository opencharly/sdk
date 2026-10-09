package loaderkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// parse_candy_manifest_fallback_test.go — the "charly.yml is everything at once"
// fallback: ParseCandyManifest must find a candy node in a PROJECT file (version/
// repo/import/discover + entity nodes) even when ParseDoc fails — the empty-Threaded
// resolver context before the provider registry is loaded.

func TestParseCandyManifest_ProjectFileFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "charly.yml")
	body := `version: 2026.232.0520
repo: github.com/opencharly/example
discover:
    - path: candy
      recursive: true
check-some-bed:
    pod:
        image: example
        disposable: true
example-candy:
    candy:
        version: 2026.232.0001
        description: a candy node in a project file
        package:
            - example
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Empty Threaded — ParseDoc fails, the fallback must find the candy node.
	ly, err := ParseCandyManifest(path, spec.Threaded{}, spec.CandyVocab{})
	if err != nil {
		t.Fatalf("ParseCandyManifest: %v", err)
	}
	if ly.Name != "example-candy" {
		t.Errorf("name = %q, want example-candy", ly.Name)
	}
	if len(ly.Package) == 0 || ly.Package[0].Name != "example" {
		t.Errorf("package = %v, want [example]", ly.Package)
	}
}

// TestParseCandyManifest_ProjectFileFallbackDesugarsPlan is sdk#323's acceptance test: the
// FALLBACK branch must run the SAME desugar step the primary (ParseDoc) branch does. The
// fallback is reached by any project file whose top-level `version:` scalar makes ParseDoc
// fail (version is no longer a DocDirective after the version-stamp retirement), so a candy
// node in such a file had its `plan:` steps decoded RAW: the authored `<word>: <input>`
// plugin-verb sugar never became the internal plugin/plugin_input pair, and the closed #Op
// decode then silently DROPPED the verb — the "a top-level version: scalar silently strips
// every authored plugin verb" defect. This test FAILS before the fix (Plugin == "" and
// PluginInput == nil) and passes after it.
func TestParseCandyManifest_ProjectFileFallbackDesugarsPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "charly.yml")
	body := `version: 2026.232.0520
repo: github.com/opencharly/example
discover:
    - path: candy
      recursive: true
example-candy:
    candy:
        version: 2026.232.0001
        description: a candy node in a project file
        package:
            - example
        plan:
            - check: the plugin verb dispatches
              examplerunverb:
                  marker: hello
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Empty Threaded — ParseDoc fails on the top-level `version:` scalar, so the fallback runs.
	ly, err := ParseCandyManifest(path, spec.Threaded{}, spec.CandyVocab{})
	if err != nil {
		t.Fatalf("ParseCandyManifest: %v", err)
	}
	if len(ly.Plan) != 1 {
		t.Fatalf("plan = %d step(s), want 1 (the fallback must decode the candy's plan)", len(ly.Plan))
	}
	st := ly.Plan[0]
	if st.Plugin != "examplerunverb" {
		t.Errorf("plan[0].Plugin = %q, want %q — the fallback branch decoded the plan WITHOUT desugarEntityPlan, so the authored plugin-verb sugar was dropped by the closed #Op decode (sdk#323)", st.Plugin, "examplerunverb")
	}
	if got := st.PluginInput["marker"]; got != "hello" {
		t.Errorf("plan[0].PluginInput[marker] = %v, want %q — the sugar's input must survive as plugin_input", got, "hello")
	}
	if st.Check == "" {
		t.Errorf("plan[0].Check = %q, want the authored intent keyword to survive", st.Check)
	}
}
