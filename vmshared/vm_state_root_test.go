package vmshared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVmStateRoot_DefaultUnchanged proves the no-env-var default path is byte-identical to the
// original hardcoded literal every call site used before this fix — the batch item 6 fix must be
// a pure override addition, zero behavior change for the common single-worktree case.
func TestVmStateRoot_DefaultUnchanged(t *testing.T) {
	t.Setenv(VmStateDirEnv, "")
	got, err := VmStateRoot()
	if err != nil {
		t.Fatalf("VmStateRoot() error = %v", err)
	}
	home, herr := os.UserHomeDir()
	if herr != nil {
		t.Fatalf("os.UserHomeDir() error = %v", herr)
	}
	want := filepath.Join(home, ".local", "share", "charly", "vm")
	if got != want {
		t.Errorf("VmStateRoot() = %q, want %q", got, want)
	}
}

// TestVmStateRoot_EnvOverride is the break-it-proven regression test for the worktree-scoping
// footgun: setting CHARLY_VM_STATE_DIR must redirect VmStateRoot entirely, so two worktrees
// pointing it at distinct paths can never collide on the same libvirt-domain state directory.
func TestVmStateRoot_EnvOverride(t *testing.T) {
	override := t.TempDir()
	t.Setenv(VmStateDirEnv, override)
	got, err := VmStateRoot()
	if err != nil {
		t.Fatalf("VmStateRoot() error = %v", err)
	}
	if got != override {
		t.Errorf("VmStateRoot() = %q, want the override %q", got, override)
	}
}

// TestVmStateRoot_RelativeOverrideRejected: a relative override would silently resolve against
// whatever cwd happens to be active at each call site (some call sites use os.Getwd() elsewhere
// in this VM subsystem) — defeating the whole point of a STABLE per-worktree pin. Must error, not
// silently accept.
func TestVmStateRoot_RelativeOverrideRejected(t *testing.T) {
	t.Setenv(VmStateDirEnv, "relative/path")
	_, err := VmStateRoot()
	if err == nil {
		t.Fatal("VmStateRoot() with a relative CHARLY_VM_STATE_DIR must error, not silently accept it")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error = %v, want it to explain the absolute-path requirement", err)
	}
}

// TestVmStateRoot_WhitespaceOnlyOverrideIgnored: an accidentally-set-but-empty env var (e.g.
// `CHARLY_VM_STATE_DIR= charly bundle add ...` from a stray shell script) must fall back to the
// default, not error or resolve to an empty/garbage path.
func TestVmStateRoot_WhitespaceOnlyOverrideIgnored(t *testing.T) {
	t.Setenv(VmStateDirEnv, "   ")
	got, err := VmStateRoot()
	if err != nil {
		t.Fatalf("VmStateRoot() error = %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "charly", "vm")
	if got != want {
		t.Errorf("VmStateRoot() with whitespace-only override = %q, want the default %q", got, want)
	}
}
