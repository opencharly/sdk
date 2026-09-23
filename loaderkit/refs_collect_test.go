package loaderkit

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/calver"
	"github.com/opencharly/spec/spec"
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

func TestVersionlessRefRoutesThroughSeam(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"test": json.RawMessage(`{"candy": ["@github.com/opencharly/plugin-deploy-vm"]}`),
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

// TestVersionlessRefFallbackUsesCachedClient — the FALLBACK regression: with the
// LatestTag seam nil, the version-less resolution must route through
// gitClient().LatestTag (the cached 1h-TTL client), never the raw refs.GitLatestTag.
// A local repo + a warmed cache + a FAILING git shim: any raw-git invocation breaks
// the test, so a pass proves the fallback served the cached tag.
func TestVersionlessRefFallbackUsesCachedClient(t *testing.T) {
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
	url := "https://github.com/opencharly/plugin-deploy-vm"
	if _, err := gitClient().LatestTag(url); err != nil {
		t.Skip("warm failed (network?): " + err.Error())
	}
	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "git")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\necho raw-git-invoked >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+":/usr/bin:/bin")
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"test": json.RawMessage(`{"candy": ["@github.com/opencharly/plugin-deploy-vm"]}`),
		},
	}
	// the seam is NIL — the fallback (gitClient().LatestTag) must serve the cached tag
	if _, err := CollectRemoteRefsOpts(cfg, nil, spec.ResolveOpts{}, spec.RefsCollectSeams{}); err != nil {
		t.Fatalf("collect with the fallback: %v", err)
	}
}

// TestDeriveRepoViewNeverMutatesPristine locks the ROOT-CAUSE fix: deriving a
// head-schema view must copy + migrate a SEPARATE directory and leave the pristine
// cache export byte-for-byte untouched. The pre-cutover code migrated the export
// IN PLACE, so a newer binary rewrote the shared cache to its own schema CalVer
// and poisoned every older consumer — the exact check-substrate deploy-add failure
// ("config schema <newer> is newer than this charly supports").
func TestDeriveRepoViewNeverMutatesPristine(t *testing.T) {
	dir := t.TempDir()
	pristine := filepath.Join(dir, "repo@main")
	if err := os.MkdirAll(pristine, 0o755); err != nil {
		t.Fatal(err)
	}
	// An OLD-schema charly.yml (behind HEAD) so the view must be derived.
	const oldSchema = "2026.100.0000"
	root := []byte("version: " + oldSchema + "\nbox:\n  b:\n    candy: [a]\n")
	if err := os.WriteFile(filepath.Join(pristine, spec.UnifiedFileName), root, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(pristine, spec.UnifiedFileName))

	var migrated []string
	migrate := func(p string) error {
		migrated = append(migrated, p)
		// Simulate the migrate engine: stamp the head schema.
		head := []byte("version: " + calver.LatestSchemaCalVer().String() + "\nbox:\n  b:\n    candy: [a]\n")
		return os.WriteFile(filepath.Join(p, spec.UnifiedFileName), head, 0o644)
	}

	view, err := DeriveRepoView(pristine, migrate)
	if err != nil {
		t.Fatalf("DeriveRepoView: %v", err)
	}
	if view == pristine {
		t.Fatal("a behind-head tree must derive a SEPARATE view, not return the pristine path")
	}
	// The migration runs ONCE, on the private staging copy (never the pristine export nor the
	// shared view path directly — publication is an atomic rename of the completed copy).
	if len(migrated) != 1 {
		t.Fatalf("migrate must run exactly once, got %v", migrated)
	}
	if migrated[0] == pristine {
		t.Fatalf("migrate ran on the PRISTINE export %s — the whole defect was mutating it in place", pristine)
	}
	// The pristine export is untouched.
	after, _ := os.ReadFile(filepath.Join(pristine, spec.UnifiedFileName))
	if string(after) != string(before) {
		t.Fatalf("pristine export was MUTATED:\n before=%q\n after =%q", before, after)
	}
	if !bytes.Contains(after, []byte(oldSchema)) {
		t.Fatalf("pristine export no longer carries its own schema: %q", after)
	}
	// The view carries the head schema and a completeness marker.
	viewRoot, _ := os.ReadFile(filepath.Join(view, spec.UnifiedFileName))
	if !bytes.Contains(viewRoot, []byte(calver.LatestSchemaCalVer().String())) {
		t.Fatalf("derived view not migrated: %q", viewRoot)
	}
	if _, err := os.Stat(filepath.Join(view, viewMarkerName)); err != nil {
		t.Fatalf("derived view missing its completeness marker: %v", err)
	}
}

// TestDeriveRepoViewReusesBuiltView proves the view is built once and reused: a
// second call does not re-run migrate and returns the same path.
func TestDeriveRepoViewReusesBuiltView(t *testing.T) {
	dir := t.TempDir()
	pristine := filepath.Join(dir, "repo@v1.0.0")
	if err := os.MkdirAll(pristine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pristine, spec.UnifiedFileName), []byte("version: 2026.100.0000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	migrate := func(p string) error {
		calls++
		return os.WriteFile(filepath.Join(p, spec.UnifiedFileName), []byte("version: "+calver.LatestSchemaCalVer().String()+"\n"), 0o644)
	}
	view1, err := DeriveRepoView(pristine, migrate)
	if err != nil {
		t.Fatal(err)
	}
	view2, err := DeriveRepoView(pristine, migrate)
	if err != nil {
		t.Fatal(err)
	}
	if view1 != view2 {
		t.Fatalf("view path changed between calls: %s vs %s", view1, view2)
	}
	if calls != 1 {
		t.Fatalf("migrate ran %d times, want 1 (a built view must be reused)", calls)
	}
}

// TestDeriveRepoViewAtHeadIsIdentity proves an already-head tree is returned as-is
// with no copy and no migrate — the fast path.
func TestDeriveRepoViewAtHeadIsIdentity(t *testing.T) {
	dir := t.TempDir()
	pristine := filepath.Join(dir, "repo@v2.0.0")
	if err := os.MkdirAll(pristine, 0o755); err != nil {
		t.Fatal(err)
	}
	head := "version: " + calver.LatestSchemaCalVer().String() + "\n"
	if err := os.WriteFile(filepath.Join(pristine, spec.UnifiedFileName), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	view, err := DeriveRepoView(pristine, func(string) error { called = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if view != pristine {
		t.Fatalf("an at-head tree must be returned as-is, got %s", view)
	}
	if called {
		t.Fatal("migrate must not run for an at-head tree")
	}
}

// TestDeriveRepoViewNeverMigratesPristineOnFailure locks the read-only guarantee
// on the FAILURE path: when the migration of the derived copy fails, the
// pristine export must still be byte-identical and migrate must never have been
// called with the pristine path. The pre-fix code fell back to migrating
// cachePath in place on a lock error — the exact shared-cache poisoning this
// function removes.
func TestDeriveRepoViewNeverMigratesPristineOnFailure(t *testing.T) {
	dir := t.TempDir()
	pristine := filepath.Join(dir, "repo@main")
	if err := os.MkdirAll(pristine, 0o755); err != nil {
		t.Fatal(err)
	}
	root := []byte("version: 2026.100.0000\nbox:\n  b:\n    candy: [a]\n")
	if err := os.WriteFile(filepath.Join(pristine, spec.UnifiedFileName), root, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(pristine, spec.UnifiedFileName))

	// Force the LOCK-acquire failure the old code fell back from: make the view
	// lock path an unopenable DIRECTORY, so AcquireFileLock errors.
	viewPath := derivedViewPath(pristine)
	if err := os.MkdirAll(viewPath+".lock", 0o755); err != nil {
		t.Fatal(err)
	}
	var sawPristine bool
	migrate := func(p string) error {
		if p == pristine {
			sawPristine = true
		}
		return nil
	}
	if _, err := DeriveRepoView(pristine, migrate); err == nil {
		t.Fatal("a lock-acquire failure must fail the derive, never mutate the pristine export")
	}
	if sawPristine {
		t.Fatal("migrate was called with the PRISTINE export on the lock-failure path — the read-only guarantee is broken")
	}
	after, _ := os.ReadFile(filepath.Join(pristine, spec.UnifiedFileName))
	if string(after) != string(before) {
		t.Fatalf("pristine export mutated on the failure path:\n before=%q\n after =%q", before, after)
	}
}
