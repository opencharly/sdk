package loaderkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
