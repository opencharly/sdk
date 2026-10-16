package candywalk

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCollectEntitiesRemote_ReportsResolutionProgress pins the diagnosability contract added for
// opencharly/docs#162 and opencharly/spec#258.
//
// The corpus walk resolves the transitive closure of every @github ref it discovers — roughly a
// thousand refs for the docs corpus — and each resolve is a network round trip. Before this line
// existed the resolution half printed NOTHING, so the phase was indistinguishable from a hang: the
// docs deploy step sat silent for tens of minutes, and characterising it took a SIGQUIT goroutine
// dump rather than a log. The download half already printed one line per repo (spec/refs'
// "Downloading %s:%s..."); this is the matching line for the resolve half.
//
// The line is emitted BEFORE the resolve, which is the property that matters: the last line in the
// log names the ref currently being worked on, so a stall is attributable to a specific repo. A
// line emitted after would name only the refs that already succeeded.
func TestCollectEntitiesRemote_ReportsResolutionProgress(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "candy", "keeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A local candy that references ONE moved remote candy, so the walk resolves exactly one ref.
	manifest := `keeper:
    candy:
        version: 2026.200.1000
        description: References one moved candy.
        candy:
            - '@github.com/opencharly/layer-ripgrep:v2026.144.1443'
`
	if err := os.WriteFile(filepath.Join(root, "candy", "keeper", "charly.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	fetched := t.TempDir()
	remote := `ripgrep:
    candy:
        version: 2026.144.1443
        description: Fast recursive text search.
`
	if err := os.WriteFile(filepath.Join(fetched, "charly.yml"), []byte(remote), 0o644); err != nil {
		t.Fatal(err)
	}

	var resolvedCalls int
	resolve := func(repoPath, version string) (string, error) {
		resolvedCalls++
		// A ref the walk got wrong must surface as a resolve error the walk reports, not as a
		// t.Fatalf inside the callback — and returning an error on the mismatch branches is
		// also what stops this double from being a function whose error result is always nil
		// (which is both a lint finding and a weaker test).
		if repoPath != "github.com/opencharly/layer-ripgrep" {
			return "", fmt.Errorf("resolver called with repo %q, want github.com/opencharly/layer-ripgrep", repoPath)
		}
		if version != "v2026.144.1443" {
			return "", fmt.Errorf("resolver called with version %q, want v2026.144.1443", version)
		}
		return fetched, nil
	}

	stderr := captureStderr(t, func() {
		if _, err := CollectEntitiesRemote([]Root{{Namespace: "", Dir: root}}, resolve); err != nil {
			t.Fatalf("CollectEntitiesRemote: %v", err)
		}
	})

	if resolvedCalls != 1 {
		t.Fatalf("resolver called %d times, want 1 — the fixture no longer resolves exactly one ref", resolvedCalls)
	}
	// The assertion that fails without the change: with no progress line, stderr is empty and this
	// reports the absence rather than passing quietly.
	lines := progressLines(stderr)
	if len(lines) != 1 {
		t.Fatalf("got %d progress line(s) on stderr, want exactly 1; stderr was:\n%s", len(lines), stderr)
	}
	line := lines[0]
	// The ref must be NAMED — that is what makes a stall attributable.
	if !strings.Contains(line, "github.com/opencharly/layer-ripgrep:v2026.144.1443") {
		t.Fatalf("progress line does not name the ref being resolved: %q", line)
	}
	// And the counters must be present, so a reader can tell progress from a stall.
	if !strings.Contains(line, "0 resolved") {
		t.Fatalf("progress line does not report the resolved count: %q", line)
	}
	if !strings.Contains(line, "queued") {
		t.Fatalf("progress line does not report the frontier size: %q", line)
	}
}

// TestCollectEntitiesRemote_NoProgressWithoutRemoteMode is the paired negative: the local-only walk
// (resolve == nil) makes no network calls, so it must print nothing. Without this, a progress line
// that fired unconditionally would look correct in the test above while polluting every local walk.
func TestCollectEntitiesRemote_NoProgressWithoutRemoteMode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "candy", "keeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `keeper:
    candy:
        version: 2026.200.1000
        description: A local candy.
`
	if err := os.WriteFile(filepath.Join(root, "candy", "keeper", "charly.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() {
		if _, err := CollectEntities([]Root{{Namespace: "", Dir: root}}); err != nil {
			t.Fatalf("CollectEntities: %v", err)
		}
	})
	if got := progressLines(stderr); len(got) != 0 {
		t.Fatalf("the local-only walk printed %d progress line(s), want 0; stderr was:\n%s", len(got), stderr)
	}
}

// captureStderr runs fn with os.Stderr redirected and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing the capture pipe: %v", err)
	}
	os.Stderr = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading the captured stderr: %v", err)
	}
	return string(out)
}

// progressLines returns the walk's own progress lines only, so an unrelated library message on the
// same stream cannot make the assertion above pass.
func progressLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "resolving ") {
			out = append(out, line)
		}
	}
	return out
}
