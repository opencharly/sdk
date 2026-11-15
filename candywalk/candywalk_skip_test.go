package candywalk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCollectEntitiesRemote_SkipsUnresolvableRef is the R7 gate on opencharly/marketplace#613.
//
// A dead ref anywhere in the transitive closure used to abort the WHOLE corpus walk. Measured:
// `pod-crabbox:v2026.274.0652` was deleted from its repo (its commit no longer exists), and
// because ~230 corpus sources pin refs transitively, one stale pin kept
// `charly marketplace generate` red for five days — every skill in the org stopped being
// republished, and the daily refresh failed every night.
//
// The contract now: a ref that cannot be resolved costs THAT ref's entities and nothing else.
// It is a loud WARNING naming the ref, plus a summary line stating how many were skipped, so a
// silently shrunk corpus is impossible to mistake for a complete one.
func TestCollectEntitiesRemote_SkipsUnresolvableRef(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "candy", "keeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Two remote refs: one resolves, one is dead. The dead one must not stop the good one.
	manifest := `keeper:
    candy:
        version: 2026.200.1000
        description: References one moved candy and one dead ref.
        candy:
            - '@github.com/opencharly/layer-good:v2026.144.1443'
            - '@github.com/opencharly/pod-dead:v2026.274.0652'
`
	if err := os.WriteFile(filepath.Join(root, "candy", "keeper", "charly.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	good := t.TempDir()
	remote := `good:
    candy:
        version: 2026.144.1443
        description: A candy that resolves.
`
	if err := os.WriteFile(filepath.Join(good, "charly.yml"), []byte(remote), 0o644); err != nil {
		t.Fatal(err)
	}

	resolve := func(repoPath, version string) (string, error) {
		if repoPath == "github.com/opencharly/pod-dead" {
			return "", fmt.Errorf("could not resolve ref %q in https://github.com/opencharly/pod-dead.git", version)
		}
		if repoPath != "github.com/opencharly/layer-good" {
			return "", fmt.Errorf("resolver called with unexpected repo %q", repoPath)
		}
		return good, nil
	}

	var ents []Entity
	stderr := captureStderr(t, func() {
		var err error
		ents, err = CollectEntitiesRemote([]Root{{Namespace: "", Dir: root}}, resolve)
		if err != nil {
			t.Fatalf("a dead ref must NOT abort the walk: %v", err)
		}
	})

	// The good ref's entity is still published: the walk continued past the dead one.
	var sawGood bool
	for _, e := range ents {
		if e.Name == "good" {
			sawGood = true
		}
	}
	if !sawGood {
		t.Fatalf("the resolvable ref's entity is missing — the walk did not continue past the dead ref; got %d entities", len(ents))
	}

	if !strings.Contains(stderr, "skipping unresolvable ref @github.com/opencharly/pod-dead:v2026.274.0652") {
		t.Fatalf("stderr must NAME the skipped ref (a silent skip is the bug this fixes); got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "1 ref(s) could not be resolved and were SKIPPED") {
		t.Fatalf("stderr must carry the ONE skip-summary line, so a shrunk corpus is visible without grepping; got:\n%s", stderr)
	}
}

// TestCollectEntitiesRemote_SkipsUnwalkableFetchedTree is the paired arm for the OTHER failure
// shape measured on the same outage: the ref resolves, but the fetched tree is unusable — a repo
// whose own manifest declares a candy it does not contain (pod-herdr declaring `candy/socat` at a
// tag with no such directory), or an unparseable candy manifest. That must be skipped and warned
// like a dead ref, not abort the walk.
func TestCollectEntitiesRemote_SkipsUnwalkableFetchedTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "candy", "keeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `keeper:
    candy:
        version: 2026.200.1000
        description: References a repo whose fetched tree cannot be walked.
        candy:
            - '@github.com/opencharly/layer-broken:v2026.144.1443'
            - '@github.com/opencharly/layer-good:v2026.144.1443'
`
	if err := os.WriteFile(filepath.Join(root, "candy", "keeper", "charly.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	good := t.TempDir()
	if err := os.WriteFile(filepath.Join(good, "charly.yml"), []byte("good:\n    candy:\n        version: 2026.144.1443\n        description: A candy that resolves.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A fetched tree whose candy manifest cannot be READ at all (here: the manifest path is a
	// directory, so os.ReadFile fails with EISDIR — the reachable error path; unparseable YAML is
	// deliberately TOLERATED upstream, and a non-catalog scalar shape is skipped).
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, "candy", "broken", "charly.yml"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolve := func(repoPath, _ string) (string, error) {
		switch repoPath {
		case "github.com/opencharly/layer-broken":
			return broken, nil
		case "github.com/opencharly/layer-good":
			return good, nil
		}
		return "", fmt.Errorf("resolver called with unexpected repo %q", repoPath)
	}

	var ents []Entity
	stderr := captureStderr(t, func() {
		var err error
		ents, err = CollectEntitiesRemote([]Root{{Namespace: "", Dir: root}}, resolve)
		if err != nil {
			t.Fatalf("an unwalkable fetched tree must NOT abort the walk: %v", err)
		}
	})

	var sawGood bool
	for _, e := range ents {
		if e.Name == "good" {
			sawGood = true
		}
	}
	if !sawGood {
		t.Fatalf("the healthy ref's entity is missing — the walk did not continue past the broken tree; got %d entities", len(ents))
	}
	if !strings.Contains(stderr, "skipping ref @github.com/opencharly/layer-broken:v2026.144.1443") {
		t.Fatalf("stderr must NAME the skipped ref; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "1 ref(s) could not be resolved and were SKIPPED") {
		t.Fatalf("stderr must carry the skip summary; got:\n%s", stderr)
	}
}
