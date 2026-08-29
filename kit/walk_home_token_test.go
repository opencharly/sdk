package kit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestWalkPlans_ResolvesHomeTokenForCopy is the regression test for the machine-venue
// home-token gap (charly#460).
//
// A home-relative `copy: to: ${HOME}/...` is TOKENIZED at compile into
// "{{.Home}}/..." so each emit target can resolve it against the destination home. The
// OCI target does that via spec.ResolveHome; the machine-venue walk did not, so the
// literal token reached PutFile and the file landed in a directory named "{{.Home}}"
// inside the venue home — silently, with no error, while the sibling mkdir/write/command
// verbs in the same candy applied normally.
//
// Without the resolution in WalkPlans this test fails on the PutFile destination.
func TestWalkPlans_ResolvesHomeTokenForCopy(t *testing.T) {
	candyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(candyDir, "marker.txt"), []byte("MARKER\n"), 0o644); err != nil {
		t.Fatalf("seed candy file: %v", err)
	}
	exec := newFakeExec() // its RunCapture answers the $HOME probe with /home/u
	plans := []spec.InstallPlanView{{
		Steps: []spec.InstallStepView{{
			Kind:      "Op",
			Scope:     spec.ScopeUser,
			CandyName: "c",
			CandyDir:  candyDir,
			To:        spec.HomeToken + "/probe/copied.txt",
			Op:        &spec.Op{Copy: "marker.txt", Mode: "0644"},
		}},
	}}
	if _, err := WalkPlans(context.Background(), exec, plans, WalkOpts{}); err != nil {
		t.Fatalf("WalkPlans: %v", err)
	}
	const want = "/home/u/probe/copied.txt"
	if _, ok := exec.puts[want]; !ok {
		t.Errorf("copy did not land at the resolved home path %q; PutFile saw %v", want, keysOf(exec.puts))
	}
	for path := range exec.puts {
		if strings.Contains(path, spec.HomeToken) {
			t.Errorf("PutFile destination still carries the unresolved token: %q", path)
		}
	}
}

// TestWalkPlans_ExplicitHomeOptWins — a caller that already knows the venue home must not
// trigger a probe, and its value must be the one substituted.
func TestWalkPlans_ExplicitHomeOptWins(t *testing.T) {
	candyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(candyDir, "m"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	exec := newFakeExec()
	plans := []spec.InstallPlanView{{
		Steps: []spec.InstallStepView{{
			Kind: "Op", Scope: spec.ScopeUser, CandyName: "c", CandyDir: candyDir,
			To: spec.HomeToken + "/m", Op: &spec.Op{Copy: "m"},
		}},
	}}
	if _, err := WalkPlans(context.Background(), exec, plans, WalkOpts{Home: "/srv/app"}); err != nil {
		t.Fatalf("WalkPlans: %v", err)
	}
	if _, ok := exec.puts["/srv/app/m"]; !ok {
		t.Errorf("explicit WalkOpts.Home not honoured; PutFile saw %v", keysOf(exec.puts))
	}
}

// TestStepCarriesHomeToken covers the lazy-probe predicate across every field family, so
// a future field addition that forgets one is caught here rather than in a venue.
func TestStepCarriesHomeToken(t *testing.T) {
	tok := spec.HomeToken
	carrying := map[string]spec.InstallStepView{
		"To":          {To: tok + "/x"},
		"Dest":        {Dest: tok + "/x"},
		"ExtractDest": {ExtractDest: tok + "/x"},
		"UnitPath":    {UnitPath: tok + "/x"},
		"UnitText":    {UnitText: "ExecStart=" + tok + "/bin/a"},
		"Snippet":     {Snippet: "export P=" + tok},
		"Destination": {Destination: tok + "/x"},
		"EnvVars":     {EnvVars: map[string]string{"P": tok + "/bin"}},
		"PathAdd":     {PathAdd: []string{tok + "/bin"}},
		"PathAppend":  {PathAppend: []string{tok + "/bin"}},
	}
	for name, step := range carrying {
		s := step
		if !stepCarriesHomeToken(&s) {
			t.Errorf("%s: token not detected", name)
		}
		resolveStepHome(&s, "/home/u")
		if stepCarriesHomeToken(&s) {
			t.Errorf("%s: token survived resolution", name)
		}
	}
	// A step with no token must not trigger a probe.
	plain := spec.InstallStepView{To: "/etc/x", UnitText: "[Unit]"}
	if stepCarriesHomeToken(&plain) {
		t.Error("a token-free step must not report as carrying one")
	}
}

// TestResolveStepHomeNoopOnEmptyHome — an unresolvable home must leave the view untouched
// rather than substituting an empty string and producing a bare "/probe/..." path.
func TestResolveStepHomeNoopOnEmptyHome(t *testing.T) {
	s := spec.InstallStepView{To: spec.HomeToken + "/probe"}
	resolveStepHome(&s, "")
	if s.To != spec.HomeToken+"/probe" {
		t.Errorf("empty home must be a no-op, got %q", s.To)
	}
}
