package kit

import (
	"strings"
	"testing"
)

// TestImageBuildLockPath proves the cross-project serialization key: every TAG
// of the same registry/name image (and every project that produces that ref)
// maps to ONE user-cache lock, while distinct image refs map to distinct locks —
// so a per-project cold build of the SAME ref into the one shared podman
// graphroot serializes instead of racing (the latent store-write race).
func TestImageBuildLockPath(t *testing.T) {
	p := func(ref string) string {
		path, err := ImageBuildLockPath(ref)
		if err != nil {
			t.Fatalf("ImageBuildLockPath(%q): %v", ref, err)
		}
		return path
	}
	base := "ghcr.io/opencharly/arch-builder"
	// Same image, different CalVer tags → SAME lock (must serialize).
	if p(base+":2026.189.0420") != p(base+":2026.100.0000") {
		t.Fatal("different tags of the same image must share one lock")
	}
	// Untagged and tagged forms of the same ref → SAME lock.
	if p(base) != p(base+":2026.189.0420") {
		t.Fatal("tagged and untagged forms of the same ref must share one lock")
	}
	// A registry:port ref must NOT be tag-stripped at the port colon.
	if p("localhost:5000/foo:1.0") == p("localhost:5000/bar:1.0") {
		t.Fatal("distinct names under a registry:port must take distinct locks")
	}
	// Distinct images → distinct locks (leaf fan-out still parallel).
	if p(base+":t") == p("ghcr.io/opencharly/fedora-builder:t") {
		t.Fatal("distinct image refs must take distinct locks")
	}
	// The lock lives under the user cache, NOT a project .build dir.
	if got := p(base); !strings.Contains(got, "/charly/locks/image-") || strings.Contains(got, "/.build/") {
		t.Fatalf("lock path must be user-cache-scoped, got %q", got)
	}
}

// TestStoreBuildLockPath proves the store-global build lock is one stable
// user-cache path (every whole-image-build invocation serializes through it),
// and that it contends like any flock (a second holder sees ErrLockBusy until
// the first releases).
func TestStoreBuildLockPath(t *testing.T) {
	path, err := StoreBuildLockPath()
	if err != nil {
		t.Fatalf("StoreBuildLockPath: %v", err)
	}
	if !strings.HasSuffix(path, "/charly/locks/store-build.lock") {
		t.Fatalf("unexpected store lock path %q", path)
	}
	release, err := AcquireStoreBuildLock()
	if err != nil {
		t.Fatalf("AcquireStoreBuildLock: %v", err)
	}
	if _, err := AcquireFileLock(path, false); err == nil {
		t.Fatal("second holder must see ErrLockBusy while the first holds the store lock")
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := AcquireFileLock(path, false); err != nil {
		t.Fatalf("lock must be acquirable after release: %v", err)
	}
}
