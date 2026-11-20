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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv(VmStateDirEnv, "")
	got, err := VmStateRoot()
	if err != nil {
		t.Fatalf("VmStateRoot() error = %v", err)
	}
	want := filepath.Join(home, ".local", "share", "charly", "vm")
	if got != want {
		t.Errorf("VmStateRoot() = %q, want %q", got, want)
	}
}

// TestVmStateRoot_FollowsXDGDataHome is the R7 control for the data-root migration:
// the pre-change code used os.UserHomeDir() + .local/share and ignored
// XDG_DATA_HOME, so this path was outside the relocated data tree.
func TestVmStateRoot_FollowsXDGDataHome(t *testing.T) {
	data := filepath.Join(t.TempDir(), "xdg-data")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv(VmStateDirEnv, "")

	got, err := VmStateRoot()
	if err != nil {
		t.Fatalf("VmStateRoot() error = %v", err)
	}
	if want := filepath.Join(data, "charly", "vm"); got != want {
		t.Errorf("VmStateRoot() = %q, want the XDG data root %q", got, want)
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
// `CHARLY_VM_STATE_DIR= charly deploy add ...` from a stray shell script) must fall back to the
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
