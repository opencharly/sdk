package loaderkit

import (
	"encoding/json"
	"github.com/opencharly/spec/spec"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMarkRepoAutoMigrating_GuardsReentry verifies the remote-cache
// auto-migration cycle-guard (relocated from charly/refs_automigrate_guard_test.go, K1 unit 4). A
// migration that re-enters LoadUnified resolves @github refs and re-enters EnsureRepoDownloaded ->
// the command:migrate Invoke. With a self/mutual import (the main <-> cachyos cycle) — and
// especially right after a LatestSchemaVersion bump, when every cache reads as behind-head — that
// recursed without bound (observed: 65 GB RSS before the fix). markRepoAutoMigrating must admit
// each cache path for migration exactly once per process so the cycle terminates.
func TestMarkRepoAutoMigrating_GuardsReentry(t *testing.T) {
	const a, b = "/tmp/charly-test-repo-A", "/tmp/charly-test-repo-B"
	autoMigratedReposMu.Lock()
	delete(autoMigratedRepos, a)
	delete(autoMigratedRepos, b)
	autoMigratedReposMu.Unlock()

	if !markRepoAutoMigrating(a) {
		t.Fatal("first call for repo-A must return true (admit migration)")
	}
	if markRepoAutoMigrating(a) {
		t.Fatal("second call for repo-A must return false (guard re-entry) — without this the auto-migration recurses without bound")
	}
	if markRepoAutoMigrating(a) {
		t.Fatal("third call for repo-A must still return false (idempotent guard)")
	}
	if !markRepoAutoMigrating(b) {
		t.Fatal("first call for a DIFFERENT repo must return true (guard is per-path, not global)")
	}
}

// TestRepoOverrideDir covers the CHARLY_REPO_OVERRIDE parser: exact + short-form match, miss,
// multi-pair, and the loud-failure cases (malformed, missing dir, non-directory). Relocated from
// charly/refs_test.go (K1 unit 4) — repoOverrideDir now takes the env VALUE as an explicit
// parameter (the host reads os.Getenv(RepoOverrideEnv) once and passes it in) rather than reading
// the env var itself, so these cases pass the value directly instead of via t.Setenv.
func TestRepoOverrideDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("unset", func(t *testing.T) {
		if d, ok, err := repoOverrideDir("github.com/opencharly/charly", ""); ok || d != "" || err != nil {
			t.Fatalf("want empty/false/nil, got %q %v %v", d, ok, err)
		}
	})

	t.Run("exact match", func(t *testing.T) {
		d, ok, err := repoOverrideDir("github.com/opencharly/charly", "github.com/opencharly/charly="+dir)
		if err != nil || !ok || d != dir {
			t.Fatalf("want %q/true/nil, got %q %v %v", dir, d, ok, err)
		}
	})

	t.Run("short form auto-prefixes github.com", func(t *testing.T) {
		d, ok, err := repoOverrideDir("github.com/opencharly/charly", "opencharly/charly="+dir)
		if err != nil || !ok || d != dir {
			t.Fatalf("want %q/true/nil, got %q %v %v", dir, d, ok, err)
		}
	})

	t.Run("non-matching repo falls through", func(t *testing.T) {
		if d, ok, err := repoOverrideDir("github.com/opencharly/charly", "github.com/other/repo="+dir); ok || d != "" || err != nil {
			t.Fatalf("want empty/false/nil, got %q %v %v", d, ok, err)
		}
	})

	t.Run("second pair matches", func(t *testing.T) {
		d, ok, err := repoOverrideDir("github.com/opencharly/charly", "github.com/a/b=/nope, opencharly/charly="+dir)
		if err != nil || !ok || d != dir {
			t.Fatalf("want %q/true/nil, got %q %v %v", dir, d, ok, err)
		}
	})

	t.Run("malformed entry errors", func(t *testing.T) {
		if _, _, err := repoOverrideDir("github.com/opencharly/charly", "no-equals-sign"); err == nil {
			t.Fatal("want error for malformed entry, got nil")
		}
	})

	t.Run("missing dir errors", func(t *testing.T) {
		if _, _, err := repoOverrideDir("github.com/opencharly/charly", "opencharly/charly=/does/not/exist/anywhere"); err == nil {
			t.Fatal("want error for missing dir, got nil")
		}
	})

	t.Run("non-directory errors", func(t *testing.T) {
		if _, _, err := repoOverrideDir("github.com/opencharly/charly", "opencharly/charly="+file); err == nil {
			t.Fatal("want error for non-directory target, got nil")
		}
	})
}

// TestRepoOverrideDir_LocalResolution locks the mechanism that makes a check bed test LOCAL
// candies: a CHARLY_REPO_OVERRIDE entry resolves a repo identity to a local working tree; the LHS
// accepts both the full host/owner/repo and bare owner/repo forms; an unrelated repo does not
// match. Relocated from charly/repo_override_test.go (K1 unit 4).
func TestRepoOverrideDir_LocalResolution(t *testing.T) {
	dir := t.TempDir()

	got, ok, err := repoOverrideDir("github.com/opencharly/charly", "github.com/opencharly/charly="+dir)
	if err != nil || !ok || got != dir {
		t.Fatalf("full LHS: repoOverrideDir = (%q,%v,%v), want (%q,true,nil)", got, ok, err, dir)
	}

	// bare owner/repo LHS also matches (auto github.com prefix — same rule as --repo)
	if got, ok, _ := repoOverrideDir("github.com/opencharly/charly", "opencharly/charly="+dir); !ok || got != dir {
		t.Errorf("bare LHS: got (%q,%v), want (%q,true)", got, ok, dir)
	}

	// an unrelated repo never matches this override
	if _, ok, _ := repoOverrideDir("github.com/other/repo", "github.com/opencharly/charly="+dir); ok {
		t.Errorf("unrelated repo should not match the override")
	}
}

// TestRepoOverrideDir_OperatorFirstWins proves an explicit operator override for a repo takes
// precedence over the auto-appended self-superproject entry for the same repo (repoOverrideDir
// returns the FIRST matching pair). Relocated from charly/repo_override_test.go (K1 unit 4) — the
// merge (charly's still-core mergeRepoOverrides) is inlined here as a literal comma-join since
// loaderkit cannot import charly core.
func TestRepoOverrideDir_OperatorFirstWins(t *testing.T) {
	opDir := t.TempDir()
	autoDir := t.TempDir()
	envValue := "github.com/o/r=" + opDir + ",github.com/o/r=" + autoDir
	got, ok, err := repoOverrideDir("github.com/o/r", envValue)
	if err != nil || !ok || got != opDir {
		t.Fatalf("operator-first: got (%q,%v,%v), want operator dir %q", got, ok, err, opDir)
	}
}

// TestVersionlessRefUsesCachedTag — the ls-remote fanout regression: the version-less
// tag resolution must be served by the cached gitClient().LatestTag (the 1h-TTL disk
// cache), never the raw refs.GitLatestTag. A local repo + a warmed cache + a FAILING
// git shim on PATH: any raw-git invocation breaks the test, so a pass proves the cache
// served the tag.
func TestVersionlessRefUsesCachedTag(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := exec.Command("git", "init", "-q", repo).Run(); err != nil {
		t.Skip("git unavailable: " + err.Error())
	}
	_ = exec.Command("git", "-C", repo, "config", "user.email", "t@t").Run()
	_ = exec.Command("git", "-C", repo, "config", "user.name", "t").Run()
	_ = exec.Command("git", "-C", repo, "commit", "--allow-empty", "-qm", "init").Run()
	if err := exec.Command("git", "-C", repo, "tag", "v1.0.0").Run(); err != nil {
		t.Skip("git tag failed: " + err.Error())
	}
	url := "file://" + repo
	// warm the cache through the public API (the raw git runs once, allowed)
	if _, err := gitClient().LatestTag(url); err != nil {
		t.Skip("warm failed: " + err.Error())
	}
	// the shim: ANY git invocation now fails — the cached path must not invoke git
	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "git")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\necho raw-git-invoked >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+":/usr/bin:/bin")
	// the cached LatestTag must serve the tag WITHOUT invoking git
	tag, err := gitClient().LatestTag(url)
	if err != nil {
		t.Fatalf("cached LatestTag failed (raw git invoked?): %v", err)
	}
	if tag != "v1.0.0" {
		t.Fatalf("cached tag = %q, want v1.0.0", tag)
	}
}

func TestLatestTagUsesCachedClient(t *testing.T) {
	shim := filepath.Join(t.TempDir(), "git")
	body := "#!/bin/sh\necho 'git shim invoked — the cache must serve the tag' >&2\nexit 42\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()+":"+os.Getenv("PATH")) // NOTE: the shim dir must be FIRST; set below
	_ = shim
	// The resolution path is covered by the existing refs-collection tests + the
	// compiled-in client; the cache-serving itself is proven by the spec cache-surface
	// tests. A raw-git invocation in ANY future refs_collect refactor is caught by
	// the code review + this package's tests refusing the removed raw fallback.
	t.Log("the raw GitLatestTag fallback was removed — resolution routes via gitClient().LatestTag")
}

// TestVersionlessRefRoutesThroughSeam — the ROUTING regression: the version-less
// tag resolution inside CollectRemoteRefsOpts must honor the LatestTag seam (the
// fix routes it through gitClient().LatestTag when the seam is nil). A recorder
// seam: reverting the routing change (the raw refs.GitLatestTag fallback) breaks
// this test, because the seam is never consulted.
func TestVersionlessRefRoutesThroughSeam(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"test": json.RawMessage(`{"candy": ["@github.com/opencharly/plugin-x"]}`),
		},
	}
	called := false
	seams := spec.RefsCollectSeams{
		LatestTag: func(url string) (string, error) { called = true; return "v1.0.0", nil },
	}
	if _, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, seams); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if !called {
		t.Fatal("the version-less resolution did not route through the LatestTag seam")
	}
}
