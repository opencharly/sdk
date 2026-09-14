package vmshared

import (
	"os"
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
	if got := VmDiskRoot(); got != "/srv/from-config" {
		t.Fatalf("VmDiskRoot() with vm.image_dir set = %q, want /srv/from-config", got)
	}
	// the env var beats the setting.
	t.Setenv(VmImageDirEnv, "/srv/from-env")
	if got := VmDiskRoot(); got != "/srv/from-env" {
		t.Fatalf("VmDiskRoot() with the env override = %q, want /srv/from-env", got)
	}
}
