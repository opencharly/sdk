package loaderkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/refs"
)

// TestReshapeViewIdentity_ReKeysOnIdentityChange — the derived-view path is
// CONTENT-ADDRESSED: a schema/loader identity change must yield a different view
// path, so a stale view is never reused. Fails without the identity-keyed path.
func TestReshapeViewIdentity_ReKeysOnIdentityChange(t *testing.T) {
	orig := reshapeViewIdentityFn
	defer func() { reshapeViewIdentityFn = orig }()

	base := reshapeViewPath("/cache/repo@v1")
	if !strings.HasSuffix(base, ".view."+reshapeViewIdentity()) {
		t.Fatalf("view path must end with .view.<identity>: %q", base)
	}
	if len(reshapeViewIdentity()) != 16 {
		t.Fatalf("reshape identity must be a 16-hex-char sha prefix, got %q", reshapeViewIdentity())
	}

	reshapeViewIdentityFn = func() string { return "deadbeefdeadbeef" }
	changed := reshapeViewPath("/cache/repo@v1")
	if changed == base {
		t.Fatal("view path must re-key when the reshape identity changes")
	}
	if !strings.HasSuffix(changed, ".view.deadbeefdeadbeef") {
		t.Fatalf("re-keyed view path = %q, want suffix .view.deadbeefdeadbeef", changed)
	}
}

// TestDeriveRepoView_BuildsOnceReusesAndLeavesPristine — the pristine shared cache is
// never mutated; the view is built exactly once and reused (marker), and a second call
// does not re-run the reshape.
func TestDeriveRepoView_BuildsOnceReusesAndLeavesPristine(t *testing.T) {
	orig := reshapeViewIdentityFn
	defer func() { reshapeViewIdentityFn = orig }()
	reshapeViewIdentityFn = func() string { return "00000000000000aa" }

	dir := t.TempDir()
	cache := filepath.Join(dir, "repo@v1")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	pristine := filepath.Join(cache, "charly.yml")
	if err := os.WriteFile(pristine, []byte("version: 2026.1.1\nname: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	migrate := func(p string) error {
		calls++
		// A stand-in reshape: drop the version: line.
		data, err := os.ReadFile(filepath.Join(p, "charly.yml"))
		if err != nil {
			return err
		}
		var kept []string
		for _, ln := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(ln, "version:") {
				kept = append(kept, ln)
			}
		}
		return os.WriteFile(filepath.Join(p, "charly.yml"), []byte(strings.Join(kept, "\n")), 0o644)
	}

	v1, err := DeriveRepoView(cache, migrate)
	if err != nil {
		t.Fatalf("DeriveRepoView: %v", err)
	}
	if calls != 1 {
		t.Fatalf("reshape ran %d times, want 1", calls)
	}
	if _, err := os.Stat(filepath.Join(v1, reshapeViewMarker)); err != nil {
		t.Fatalf("view marker missing: %v", err)
	}
	// The view is reshaped (no version line).
	if got, _ := os.ReadFile(filepath.Join(v1, "charly.yml")); strings.Contains(string(got), "version:") {
		t.Fatalf("view not reshaped: %s", got)
	}

	v2, err := DeriveRepoView(cache, migrate)
	if err != nil {
		t.Fatalf("DeriveRepoView (2nd): %v", err)
	}
	if v1 != v2 {
		t.Fatalf("second call returned a different view: %q vs %q", v1, v2)
	}
	if calls != 1 {
		t.Fatalf("reshape ran %d times after reuse, want 1 (marker must short-circuit)", calls)
	}

	// PRISTINE cache untouched: the version line is still there, and no marker leaked in.
	if got, _ := os.ReadFile(pristine); !strings.Contains(string(got), "version:") {
		t.Fatalf("pristine cache was mutated: %s", got)
	}
	if _, err := os.Stat(filepath.Join(cache, reshapeViewMarker)); err == nil {
		t.Fatal("marker must NOT be written into the pristine cache")
	}
}

// TestDeriveRepoView_ReentryGuardReturnsPristine — the reshape re-enters LoadUnified, which
// re-enters DeriveRepoView for the SAME view (a self/mutual import cycle such as
// main <-> cachyos). The re-entrant call must NOT rebuild or block: the guard returns the
// pristine cache (no view is published yet). Fails if the re-entry guard is dropped (the
// inner call would re-acquire the view flock and deadlock, or rebuild).
func TestDeriveRepoView_ReentryGuardReturnsPristine(t *testing.T) {
	orig := reshapeViewIdentityFn
	defer func() { reshapeViewIdentityFn = orig }()
	reshapeViewIdentityFn = func() string { return "00000000000000bb" }

	dir := t.TempDir()
	cache := filepath.Join(dir, "repo@v1")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "charly.yml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var reentered string
	var migrate func(string) error
	migrate = func(_ string) error {
		// Re-enter mid-reshape (the cycle). Must return the pristine cache, not block/rebuild.
		v, err := DeriveRepoView(cache, migrate)
		if err != nil {
			return err
		}
		reentered = v
		return nil
	}

	v, err := DeriveRepoView(cache, migrate)
	if err != nil {
		t.Fatalf("DeriveRepoView: %v", err)
	}
	if reentered != cache {
		t.Fatalf("re-entrant DeriveRepoView returned %q, want the pristine cache %q (the guard must short-circuit)", reentered, cache)
	}
	if v == cache {
		t.Fatalf("outer DeriveRepoView returned the pristine path; it must publish a view")
	}
	if _, err := os.Stat(filepath.Join(v, reshapeViewMarker)); err != nil {
		t.Fatalf("outer view not published (marker missing): %v", err)
	}
}

// TestDeriveRepoView_ReDerivesWhenMutableRefAdvances is the sdk#327 regression guard.
// A view of a MUTABLE ref (the default branch / any branch) is keyed on the resolved
// COMMIT, not the ref name: when upstream advances (the pristine export's provenance
// commit changes) the view MUST be re-derived, so a stale view is never served.
//
// It FAILS against the pre-fix code (the marker was the bare identity, and reuse keyed
// only on the marker's presence, so the second call returned the STALE view while the
// pristine export had already advanced).
func TestDeriveRepoView_ReDerivesWhenMutableRefAdvances(t *testing.T) {
	orig := reshapeViewIdentityFn
	defer func() { reshapeViewIdentityFn = orig }()
	reshapeViewIdentityFn = func() string { return "00000000000000cc" }

	dir := t.TempDir()
	cache := filepath.Join(dir, "repo@main")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	writeContent := func(marker string) {
		if err := os.WriteFile(filepath.Join(cache, "content.txt"), []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Upstream commit A.
	writeContent("A")
	if err := refs.WriteRepoCacheProvenance(cache, "commit-A"); err != nil {
		t.Fatal(err)
	}

	// A reshape that COPIES the pristine content into the view (so we can see staleness).
	migrate := func(p string) error {
		data, err := os.ReadFile(filepath.Join(p, "content.txt"))
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(p, "content.txt"), data, 0o644)
	}

	calls := 0
	counted := func(p string) error { calls++; return migrate(p) }

	v1, err := DeriveRepoView(cache, counted)
	if err != nil {
		t.Fatalf("DeriveRepoView: %v", err)
	}
	if calls != 1 {
		t.Fatalf("first derive ran %d times, want 1", calls)
	}
	// The marker carries the resolved COMMIT (the mutable-ref stamp), not the bare identity.
	marker, _ := os.ReadFile(filepath.Join(v1, reshapeViewMarker))
	if got := strings.TrimSpace(string(marker)); got != "commit-A" {
		t.Fatalf("view marker = %q, want the resolved commit commit-A", got)
	}
	if got, _ := os.ReadFile(filepath.Join(v1, "content.txt")); string(got) != "A" {
		t.Fatalf("view content = %q, want A", got)
	}

	// Upstream ADVANCES: the pristine export is re-fetched to commit B (new content).
	writeContent("B")
	if err := refs.WriteRepoCacheProvenance(cache, "commit-B"); err != nil {
		t.Fatal(err)
	}
	// The advance is observed on the NEXT charly process. autoMigratedRepos is a
	// once-per-view-per-process re-entry guard, so reset it to model a fresh process
	// (the pre-fix code would then blindly reuse the commit-A view; the fix re-derives).
	autoMigratedReposMu.Lock()
	autoMigratedRepos = map[string]bool{}
	autoMigratedReposMu.Unlock()

	// The next derive MUST re-derive against commit B — NOT reuse the commit-A view.
	v2, err := DeriveRepoView(cache, counted)
	if err != nil {
		t.Fatalf("DeriveRepoView (after advance): %v", err)
	}
	if calls != 2 {
		t.Fatalf("derive ran %d times after the ref advanced, want 2 (the stale view was reused)", calls)
	}
	if v2 != v1 {
		t.Fatalf("view path changed on advance: %q vs %q (the commit stamp must re-derive IN PLACE, not re-key the path)", v1, v2)
	}
	if got, _ := os.ReadFile(filepath.Join(v2, "content.txt")); string(got) != "B" {
		t.Fatalf("view content after advance = %q, want B (stale commit-A view was served)", got)
	}
	marker2, _ := os.ReadFile(filepath.Join(v2, reshapeViewMarker))
	if got := strings.TrimSpace(string(marker2)); got != "commit-B" {
		t.Fatalf("view marker after advance = %q, want commit-B", got)
	}

	// A third call at the SAME commit B reuses the fresh view (no further rebuild).
	// Clear the once-per-view guard to model the next process, then re-derive: the
	// marker's commit-B stamp must short-circuit (no rebuild at an unchanged commit).
	autoMigratedReposMu.Lock()
	autoMigratedRepos = map[string]bool{}
	autoMigratedReposMu.Unlock()
	if _, err := DeriveRepoView(cache, counted); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("derive ran %d times at the same commit, want 2 (marker must short-circuit)", calls)
	}

	// PRISTINE cache untouched: provenance + content are the resolved commit B's.
	if p, ok := refs.ReadRepoCacheProvenance(cache); !ok || p.Commit != "commit-B" {
		t.Fatalf("pristine provenance mutated: %+v ok=%v", p, ok)
	}
	if got, _ := os.ReadFile(filepath.Join(cache, "content.txt")); string(got) != "B" {
		t.Fatalf("pristine content mutated: %q", got)
	}
}

// TestDeriveRepoView_TaggedRefStillReuses — an IMMUTABLE ref (a CalVer tag) carries no
// mutable commit to advance, so a provenance-less tag export still keys on the identity
// and is reused across calls (NOT re-derived every time). Proves the fix does not turn
// every tagged view into a rebuild.
func TestDeriveRepoView_TaggedRefStillReuses(t *testing.T) {
	orig := reshapeViewIdentityFn
	defer func() { reshapeViewIdentityFn = orig }()
	reshapeViewIdentityFn = func() string { return "00000000000000dd" }

	dir := t.TempDir()
	cache := filepath.Join(dir, "repo@v2026.1.1")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "charly.yml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	migrate := func(string) error { calls++; return nil }

	if _, err := DeriveRepoView(cache, migrate); err != nil {
		t.Fatal(err)
	}
	if _, err := DeriveRepoView(cache, migrate); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("tagged view re-derived %d times, want 1 (must reuse on the identity stamp)", calls)
	}
}
