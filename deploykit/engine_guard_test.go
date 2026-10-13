package deploykit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// engineUsable reports whether the named engine can actually be used, with the reason when it
// cannot. Presence alone is not usability (opencharly/sdk#364): a host whose engine is installed
// but cannot initialise passes exec.LookPath and then fails engine-backed tests whose subject has
// nothing to do with the engine. Bounded, because a wedged engine must not hang the suite - not
// answering is not usability.
//
// A predicate rather than an inline check so the SKIP path is testable: requireUsableEngine calls
// it and skips; a test calls it and asserts, which is the half of R7 a bare skip cannot provide.
func engineUsable(engine string) (bool, string) {
	if _, err := exec.LookPath(engine); err != nil {
		return false, "not installed: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, engine, "info").CombinedOutput(); err != nil {
		return false, fmt.Sprintf("installed but unusable: %v\n%s", err, out)
	}
	return true, ""
}

// requireUsableEngine skips the calling test when the named container engine cannot be used, and
// says why - with the engine's own output. See engineUsable for the reasoning.
func requireUsableEngine(t *testing.T, engine string) {
	t.Helper()
	if ok, why := engineUsable(engine); !ok {
		t.Skipf("%s — skipping engine-backed integration test: %s", engine, why)
	}
}

// TestEngineUsable_EmptyPathIsNotUsable is the negative half of the proof: with an emptied PATH no
// engine is usable, which is the state the three guards used to handle by presence and the state a
// present-but-broken engine shares. It fails without engineUsable, because there was nothing to call.
func TestEngineUsable_EmptyPathIsNotUsable(t *testing.T) {
	withEmptyPATH(t)
	ok, why := engineUsable("podman")
	if ok {
		t.Error("engineUsable with an emptied PATH = true, want false")
	}
	if why == "" {
		t.Error("engineUsable returned no reason; a silent skip is the defect this row is about")
	}
	if _, err := exec.LookPath("podman"); err == nil {
		t.Error("withEmptyPATH did not empty PATH; the assertion above proves nothing")
	}
}

// TestEngineUsable_PresentButBrokenIsNotUsable is the assertion that distinguishes presence from usability, and
// the one that fails without the fix: a `podman` that exists on PATH and cannot run is exactly the host class this
// row was filed from (`Failed to obtain podman configuration: …`). Presence-only logic returns true here.
//
// The stub is a two-line script in a temp dir, put first on PATH. It is not a fake of a live service - nothing is
// contacted - it is a binary that exists and fails, which is the condition under test.
func TestEngineUsable_PresentButBrokenIsNotUsable(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "podman")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("writing stub engine: %v", err)
	}
	t.Setenv("PATH", dir)
	if _, err := exec.LookPath("podman"); err != nil {
		t.Fatalf("stub engine is not resolvable, so this test proves nothing: %v", err)
	}
	ok, why := engineUsable("podman")
	if ok {
		t.Error("engineUsable with an installed but failing engine = true, want false — this is the defect opencharly/sdk#364 filed")
	}
	if !strings.Contains(why, "unusable") {
		t.Errorf("reason = %q, want it to say the engine is unusable rather than missing", why)
	}
}
