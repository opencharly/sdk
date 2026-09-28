package loaderkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestRepoOverrideDir covers the CHARLY_REPO_OVERRIDE parser: exact + short-form match, miss,
// multi-pair, and the loud-failure cases (malformed, missing dir, non-directory). Relocated from
// charly/refs_test.go (K1 unit 4) — RepoOverrideDir now takes the env VALUE as an explicit
// parameter (the host reads os.Getenv(RepoOverrideEnv) once and passes it in) rather than reading
// the env var itself, so these cases pass the value directly instead of via t.Setenv.
func TestRepoOverrideDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("unset", func(t *testing.T) {
		if d, ok, err := RepoOverrideDir("github.com/opencharly/charly", ""); ok || d != "" || err != nil {
			t.Fatalf("want empty/false/nil, got %q %v %v", d, ok, err)
		}
	})

	t.Run("exact match", func(t *testing.T) {
		d, ok, err := RepoOverrideDir("github.com/opencharly/charly", "github.com/opencharly/charly="+dir)
		if err != nil || !ok || d != dir {
			t.Fatalf("want %q/true/nil, got %q %v %v", dir, d, ok, err)
		}
	})

	t.Run("short form auto-prefixes github.com", func(t *testing.T) {
		d, ok, err := RepoOverrideDir("github.com/opencharly/charly", "opencharly/charly="+dir)
		if err != nil || !ok || d != dir {
			t.Fatalf("want %q/true/nil, got %q %v %v", dir, d, ok, err)
		}
	})

	t.Run("non-matching repo falls through", func(t *testing.T) {
		if d, ok, err := RepoOverrideDir("github.com/opencharly/charly", "github.com/other/repo="+dir); ok || d != "" || err != nil {
			t.Fatalf("want empty/false/nil, got %q %v %v", d, ok, err)
		}
	})

	t.Run("second pair matches", func(t *testing.T) {
		d, ok, err := RepoOverrideDir("github.com/opencharly/charly", "github.com/a/b=/nope, opencharly/charly="+dir)
		if err != nil || !ok || d != dir {
			t.Fatalf("want %q/true/nil, got %q %v %v", dir, d, ok, err)
		}
	})

	t.Run("malformed entry errors", func(t *testing.T) {
		if _, _, err := RepoOverrideDir("github.com/opencharly/charly", "no-equals-sign"); err == nil {
			t.Fatal("want error for malformed entry, got nil")
		}
	})

	t.Run("missing dir errors", func(t *testing.T) {
		if _, _, err := RepoOverrideDir("github.com/opencharly/charly", "opencharly/charly=/does/not/exist/anywhere"); err == nil {
			t.Fatal("want error for missing dir, got nil")
		}
	})

	t.Run("non-directory errors", func(t *testing.T) {
		if _, _, err := RepoOverrideDir("github.com/opencharly/charly", "opencharly/charly="+file); err == nil {
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

	got, ok, err := RepoOverrideDir("github.com/opencharly/charly", "github.com/opencharly/charly="+dir)
	if err != nil || !ok || got != dir {
		t.Fatalf("full LHS: RepoOverrideDir = (%q,%v,%v), want (%q,true,nil)", got, ok, err, dir)
	}

	// bare owner/repo LHS also matches (auto github.com prefix — same rule as --repo)
	if got, ok, _ := RepoOverrideDir("github.com/opencharly/charly", "opencharly/charly="+dir); !ok || got != dir {
		t.Errorf("bare LHS: got (%q,%v), want (%q,true)", got, ok, dir)
	}

	// an unrelated repo never matches this override
	if _, ok, _ := RepoOverrideDir("github.com/other/repo", "github.com/opencharly/charly="+dir); ok {
		t.Errorf("unrelated repo should not match the override")
	}
}

// TestRepoOverrideDir_OperatorFirstWins proves an explicit operator override for a repo takes
// precedence over the auto-appended self-superproject entry for the same repo (RepoOverrideDir
// returns the FIRST matching pair). Relocated from charly/repo_override_test.go (K1 unit 4) — the
// merge (charly's still-core mergeRepoOverrides) is inlined here as a literal comma-join since
// loaderkit cannot import charly core.
func TestRepoOverrideDir_OperatorFirstWins(t *testing.T) {
	opDir := t.TempDir()
	autoDir := t.TempDir()
	envValue := "github.com/o/r=" + opDir + ",github.com/o/r=" + autoDir
	got, ok, err := RepoOverrideDir("github.com/o/r", envValue)
	if err != nil || !ok || got != opDir {
		t.Fatalf("operator-first: got (%q,%v,%v), want operator dir %q", got, ok, err, opDir)
	}
}

// TestCachedDefaultBranchServesWithoutGit — the ls-remote fanout regression: resolving a
// repo's default branch must be served by the cached gitClient().DefaultBranch (disk cache),
// never a raw ls-remote. This exercises the CLIENT cache the resolver uses, not the resolver
// itself (the resolver is covered by the seam tests). A warmed cache + a FAILING git shim on
// PATH: any raw-git invocation breaks the test, so a pass proves the cache served.
func TestCachedDefaultBranchServesWithoutGit(t *testing.T) {
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
	warm, err := gitClient().DefaultBranch(url)
	if err != nil {
		t.Skip("warm failed: " + err.Error())
	}
	// the shim: ANY git invocation now fails — the cached path must not invoke git
	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "git")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\necho raw-git-invoked >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+":/usr/bin:/bin")
	// the cached DefaultBranch must serve the SAME value WITHOUT invoking git
	branch, err := gitClient().DefaultBranch(url)
	if err != nil {
		t.Fatalf("cached DefaultBranch failed (raw git invoked?): %v", err)
	}
	if branch != warm {
		t.Fatalf("cached default branch = %q; want the warmed %q", branch, warm)
	}
}

func TestVersionlessRefRoutesThroughSeam(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"test": json.RawMessage(`{"candy": ["@github.com/opencharly/plugin-deploy-vm"]}`),
		},
	}
	called := false
	orig := resolveDefaultBranch
	resolveDefaultBranch = func(url string) (string, error) { called = true; return "main", nil }
	t.Cleanup(func() { resolveDefaultBranch = orig })
	if _, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{}); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if !called {
		t.Fatal("the version-less resolution did not route through the resolveDefaultBranch seam")
	}
}

// TestVersionlessRefFallbackUsesCachedClient — the FALLBACK regression: the version-less
// resolution's default resolver routes through gitClient().DefaultBranch (the cached
// client), never a raw ls-remote. A local repo + a warmed cache + a FAILING git shim:
// any raw-git invocation breaks the test, so a pass proves the fallback served the cache.
func TestVersionlessRefFallbackUsesCachedClient(t *testing.T) {
	// LIVE-OR-SKIP (R7a): exercises the REAL version-less resolution end-to-end
	// (network at warm time; SKIPS visibly when the repo is unreachable — never a
	// silent pass). Shape: (1) run the collector on the real PATH to WARM the cached
	// default branch and capture the resolved version; (2) install a FAILING git shim;
	// (3) re-run the collector — a pass proves the changed path served the CACHED
	// default branch with no raw git, and pins the resolved version to the warm value.
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"test": json.RawMessage(`{"candy": ["@github.com/opencharly/plugin-deploy-vm"]}`),
		},
	}
	const repoPath = "github.com/opencharly/plugin-deploy-vm"
	resolve := func(t *testing.T) (string, bool) {
		t.Helper()
		downloads, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{})
		if err != nil {
			return "", false
		}
		for _, d := range downloads {
			if d.RepoPath == repoPath {
				return d.Version, true
			}
		}
		return "", false
	}
	warm, ok := resolve(t)
	if !ok {
		t.Skip("warm failed (network/repo unreachable?) — live-or-skip")
	}
	if warm == "" || strings.HasPrefix(warm, "v") {
		t.Fatalf("version-less ref resolved to %q; want a default BRANCH (non-empty, not a v-tag)", warm)
	}
	// The shim: ANY git invocation now fails — the cached path must not invoke git.
	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "git")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\necho raw-git-invoked >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+":/usr/bin:/bin")
	got, ok := resolve(t)
	if !ok {
		t.Fatalf("second collect with the git shim failed — the cached default branch was NOT served (raw git invoked?)")
	}
	if got != warm {
		t.Fatalf("second collect resolved %q; want the warmed default branch %q", got, warm)
	}
}
