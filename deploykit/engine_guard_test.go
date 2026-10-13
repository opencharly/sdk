package deploykit

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// requireUsableEngine skips the calling test when the named container engine cannot be
// used, and says why — with the engine's own output.
//
// Why not exec.LookPath: presence is not usability. A host whose engine is installed but
// cannot initialise (a stale runtime dir, a socket the kernel refuses, a broken
// configuration) passes a LookPath check and then fails three integration tests with an
// error that has nothing to do with the code under test:
//
//	Failed to obtain podman configuration: set sticky bit on: chmod /run/user/1000/libpod: read-only file system
//
// That is opencharly/sdk#364. The probe below runs the engine's own `info` — the same call a
// human would make — and skips VISIBLY with the engine's stderr, so a broken engine produces
// an honest skip on every host instead of three red tests on one.
//
// Bounded, because a wedged engine must not hang the suite: the probe is given a ceiling and
// a timeout is a skip too (the engine did not answer, which is not usability).
func requireUsableEngine(t *testing.T, engine string) {
	t.Helper()
	if _, err := exec.LookPath(engine); err != nil {
		t.Skipf("no %s on this host — skipping engine-backed integration test: %v", engine, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, engine, "info").CombinedOutput(); err != nil {
		t.Skipf("%s is installed but not usable on this host — skipping engine-backed integration test: %v\n%s",
			engine, err, out)
	}
}
