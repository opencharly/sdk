package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildActivityDir_HonoursTheOneCacheEnv pins the D-STORE-1 / TODO 7.2 boundary for
// this path: `CHARLY_CACHE_DIR` is the SINGLE override of the cache root, so every
// caller-built cache path must land under it. A hand-built `$XDG_CACHE_HOME/charly`
// passes the no-override case and silently fails this one.
func TestBuildActivityDir_HonoursTheOneCacheEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHARLY_CACHE_DIR", root)

	dir, err := BuildActivityDir()
	if err != nil {
		t.Fatalf("BuildActivityDir: %v", err)
	}
	want := filepath.Join(root, "locks", "builds")
	if dir != want {
		t.Fatalf("BuildActivityDir() = %q, want %q (CHARLY_CACHE_DIR is the one cache root)", dir, want)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("BuildActivityDir did not create %q: %v", dir, err)
	}
}

// TestBuildActivityDir_NoOverrideIsTheUserCacheRoot keeps the no-override shape pinned,
// so the change above cannot relocate the default install.
func TestBuildActivityDir_NoOverrideIsTheUserCacheRoot(t *testing.T) {
	t.Setenv("CHARLY_CACHE_DIR", "")
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache dir: %v", err)
	}

	dir, err := BuildActivityDir()
	if err != nil {
		t.Fatalf("BuildActivityDir: %v", err)
	}
	if want := filepath.Join(cache, "charly", "locks", "builds"); dir != want {
		t.Fatalf("BuildActivityDir() = %q, want %q", dir, want)
	}
}
