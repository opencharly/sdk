package vmshared

import (
	"path/filepath"
	"testing"
)

// TestVmDiskRoot_Configurable asserts the VM image root is CONFIGURABLE and never
// a hardcoded literal: the env override wins, and the default is the obvious
// "image" (not the former "output", which read as disposable scratch).
func TestVmDiskRoot_Configurable(t *testing.T) {
	// default (no env, no config): the built-in "image".
	t.Setenv(VmImageDirEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate from any real vm.image_dir config
	if got := VmDiskRoot(); got != "image" {
		t.Fatalf("VmDiskRoot() default = %q, want %q", got, "image")
	}
	if got := VmDiskDir("cachyos-gpu"); got != filepath.Join("image", "cachyos-gpu") {
		t.Fatalf("VmDiskDir default = %q, want %q", got, filepath.Join("image", "cachyos-gpu"))
	}

	// env override wins.
	t.Setenv(VmImageDirEnv, "/srv/vm-images")
	if got := VmDiskRoot(); got != "/srv/vm-images" {
		t.Fatalf("VmDiskRoot() env override = %q, want %q", got, "/srv/vm-images")
	}
	if got := VmDiskDir("x"); got != filepath.Join("/srv/vm-images", "x") {
		t.Fatalf("VmDiskDir env override = %q", got)
	}
}

// TestVmDiskDir_PerVM asserts the per-VM namespace survives the configurable
// root: two VMs never share a disk dir (the regression that made one VM adopt a
// sibling's stale seed.iso).
func TestVmDiskDir_PerVM(t *testing.T) {
	t.Setenv(VmImageDirEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := VmDiskDir("cachyos-gpu")
	b := VmDiskDir("cachyos-gpu-vm")
	if a == b {
		t.Fatalf("VmDiskDir must be per-VM; got identical paths: %s", a)
	}
}
