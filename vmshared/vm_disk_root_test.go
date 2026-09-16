package vmshared

import (
	"os"
	"path/filepath"
	"testing"
)

// mustRoot / mustDir wrap the (string, error) resolvers for the tests.
func mustRoot(t *testing.T) string {
	t.Helper()
	got, err := VmDiskRoot()
	if err != nil {
		t.Fatalf("VmDiskRoot: %v", err)
	}
	return got
}

func mustDir(t *testing.T, vm string) string {
	t.Helper()
	got, err := VmDiskDir(vm)
	if err != nil {
		t.Fatalf("VmDiskDir(%q): %v", vm, err)
	}
	return got
}

// TestVmDiskRoot_Configurable asserts the VM image root is CONFIGURABLE and never
// a hardcoded literal: the env override wins, and the default is the obvious
// "image" (not the former "output", which read as disposable scratch).
func TestVmDiskRoot_Configurable(t *testing.T) {
	t.Setenv(VmImageDirEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate from any real vm.image_dir config
	if got := mustRoot(t); got != "image" {
		t.Fatalf("VmDiskRoot() default = %q, want %q", got, "image")
	}
	if got := mustDir(t, "cachyos-gpu"); got != filepath.Join("image", "cachyos-gpu") {
		t.Fatalf("VmDiskDir default = %q, want %q", got, filepath.Join("image", "cachyos-gpu"))
	}

	// env override wins.
	t.Setenv(VmImageDirEnv, "/srv/vm-images")
	if got := mustRoot(t); got != "/srv/vm-images" {
		t.Fatalf("VmDiskRoot() env override = %q, want %q", got, "/srv/vm-images")
	}
	if got := mustDir(t, "x"); got != filepath.Join("/srv/vm-images", "x") {
		t.Fatalf("VmDiskDir env override = %q", got)
	}
}

// TestVmDiskDir_PerVM asserts the per-VM namespace survives the configurable
// root: two VMs never share a disk dir (the regression that made one VM adopt a
// sibling's stale seed.iso).
func TestVmDiskDir_PerVM(t *testing.T) {
	t.Setenv(VmImageDirEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := mustDir(t, "cachyos-gpu")
	b := mustDir(t, "cachyos-gpu-vm")
	if a == b {
		t.Fatalf("VmDiskDir must be per-VM; got identical paths: %s", a)
	}
}

// TestVmDiskRoot_ConfigSetting proves the `vm.image_dir` runtime setting is
// honored, and that the env var still WINS over it — the full precedence chain.
func TestVmDiskRoot_ConfigSetting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv(VmImageDirEnv, "")
	if err := os.MkdirAll(filepath.Join(home, "charly"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "vm:\n  image_dir: /srv/from-config\n"
	if err := os.WriteFile(filepath.Join(home, "charly", "config.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// the setting beats the default.
	if got := mustRoot(t); got != "/srv/from-config" {
		t.Fatalf("VmDiskRoot() with vm.image_dir set = %q, want /srv/from-config", got)
	}
	// the env var beats the setting.
	t.Setenv(VmImageDirEnv, "/srv/from-env")
	if got := mustRoot(t); got != "/srv/from-env" {
		t.Fatalf("VmDiskRoot() with the env override = %q, want /srv/from-env", got)
	}
}

// TestVmDiskPath_AbsoluteRoot pins the absolute-root fix: an absolute vm.image_dir
// must be used AS-IS. filepath.Join does NOT reset on an absolute element, so the
// former cwd-gluing stat()ed "cwd + /srv/..." and never found the disk — breaking the
// exact relocation the configurable root exists to enable. This FAILS without the
// IsAbs guard.
func TestVmDiskPath_AbsoluteRoot(t *testing.T) {
	root := t.TempDir() // ABSOLUTE
	t.Setenv(VmImageDirEnv, root)
	vm := "abs-root-vm"
	dir := filepath.Join(root, vm)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "disk.qcow2"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// chdir elsewhere so a cwd-glue bug would look in the WRONG place.
	t.Chdir(t.TempDir())
	got, err := vmDiskPath(vm)
	if err != nil {
		t.Fatalf("vmDiskPath with an absolute root: %v", err)
	}
	if want := filepath.Join(root, vm, "disk.qcow2"); got != want {
		t.Fatalf("vmDiskPath = %q, want %q", got, want)
	}
}
