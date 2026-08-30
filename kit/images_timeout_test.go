package kit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRemoveImagesByReference_FailsFastOnHungEngine is the regression guard for
// the podman-saturation stall: a container engine that never answers (a
// saturated podman daemon) must not hang the caller forever — the engine
// command is bounded by engineCommandTimeout and fails fast (best-effort
// cleanup skips the timed-out list/rmi).
func TestRemoveImagesByReference_FailsFastOnHungEngine(t *testing.T) {
	old := engineCommandTimeout
	engineCommandTimeout = 200 * time.Millisecond
	defer func() { engineCommandTimeout = old }()

	// A fake engine binary that hangs forever (sleeps) — the shape of a
	// saturated podman daemon.
	dir := t.TempDir()
	engine := filepath.Join(dir, "hung-engine")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		RemoveImagesByReference(engine, "test-overlay")
		close(done)
	}()

	select {
	case <-done:
		// Pass: the bounded call returned instead of hanging.
	case <-time.After(5 * time.Second):
		t.Fatal("RemoveImagesByReference with a hung engine: did not return within 5s (unbounded hang)")
	}
}
