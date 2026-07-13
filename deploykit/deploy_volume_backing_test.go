package deploykit

import (
	"path/filepath"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
)

// Volume-backing + enc-path resolver tests, folded here from charly/deploy_test.go +
// charly/enc_test.go with the P11 enc-model relocation (tests follow the code, R5).

func TestEncryptedVolumeName(t *testing.T) {
	for _, tt := range []struct{ image, name, want string }{
		{"myapp", "secrets", "charly-myapp-secrets"},
		{"openclaw", "data", "charly-openclaw-data"},
	} {
		if got := EncryptedVolumeName(tt.image, tt.name); got != tt.want {
			t.Errorf("EncryptedVolumeName(%q, %q) = %q, want %q", tt.image, tt.name, got, tt.want)
		}
	}
}

func TestEncryptedCipherDir(t *testing.T) {
	if got, want := EncryptedCipherDir("/data/enc", "myapp", "secrets"), "/data/enc/charly-myapp-secrets/cipher"; got != want {
		t.Errorf("EncryptedCipherDir() = %q, want %q", got, want)
	}
}

func TestEncryptedPlainDir(t *testing.T) {
	if got, want := EncryptedPlainDir("/data/enc", "myapp", "secrets"), "/data/enc/charly-myapp-secrets/plain"; got != want {
		t.Errorf("EncryptedPlainDir() = %q, want %q", got, want)
	}
}

// TestResolveVolumeBacking_HostPath locks the bind/encrypted host-path strategy
// (resolveVolumeHostPath) — the single helper ResolveVolumeBacking's two passes
// (label-matched + deploy-only) share. Each case asserts the correct branch.
func TestResolveVolumeBacking_HostPath(t *testing.T) {
	const storageDir, encPath, volsPath = "charly-app-data", "/enc", "/vols"
	for _, c := range []struct {
		name string
		dv   vmshared.DeployVolumeConfig
		want string
	}{
		{"bind-default", vmshared.DeployVolumeConfig{Type: "bind"}, filepath.Join(volsPath, storageDir, "data")},
		{"bind-host", vmshared.DeployVolumeConfig{Type: "bind", Host: "/srv/data"}, kit.ExpandHostHome("/srv/data")},
		{"encrypted-default", vmshared.DeployVolumeConfig{Type: "encrypted"}, EncryptedPlainDir(encPath, storageDir, "data")},
		{"encrypted-host", vmshared.DeployVolumeConfig{Type: "encrypted", Host: "/srv/sec"}, filepath.Join(kit.ExpandHostHome("/srv/sec"), "plain")},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveVolumeHostPath(c.dv, "data", storageDir, encPath, volsPath); got != c.want {
				t.Errorf("resolveVolumeHostPath = %q, want %q", got, c.want)
			}
		})
	}
}

// TestResolveVolumeBacking exercises the full backing resolution: a named volume with
// no deploy override stays a named volume; a bind/encrypted override becomes a bind
// mount; a deploy-only volume (no matching label, carries Path) is added as a bind.
func TestResolveVolumeBacking(t *testing.T) {
	const boxName, instance = "app", ""
	labelVols := []VolumeMount{
		{VolumeName: DeployVolumePrefix(boxName, instance) + "data", ContainerPath: "/data"},
		{VolumeName: DeployVolumePrefix(boxName, instance) + "cache", ContainerPath: "/cache"},
	}
	deployVols := []vmshared.DeployVolumeConfig{
		{Name: "data", Type: "bind", Host: "/srv/data"},    // override → bind mount
		{Name: "logs", Type: "bind", Path: "/var/log/app"}, // deploy-only → bind mount
		// "cache" has no deploy override → stays a named volume
	}
	volumes, binds := ResolveVolumeBacking(boxName, instance, labelVols, deployVols, "/home/user", "/enc", "/vols")

	if len(volumes) != 1 || volumes[0].VolumeName != DeployVolumePrefix(boxName, instance)+"cache" {
		t.Fatalf("named volumes = %+v, want only the un-overridden cache", volumes)
	}
	got := map[string]ResolvedBindMount{}
	for _, b := range binds {
		got[b.Name] = b
	}
	if b, ok := got["data"]; !ok || b.HostPath != kit.ExpandHostHome("/srv/data") || b.ContPath != "/data" {
		t.Errorf("label-matched bind 'data' = %+v, want host=%q cont=/data", got["data"], kit.ExpandHostHome("/srv/data"))
	}
	if b, ok := got["logs"]; !ok || b.HostPath != filepath.Join("/vols", DeployStorageDir(boxName, instance), "logs") {
		t.Errorf("deploy-only bind 'logs' = %+v, want default per-deploy host path", got["logs"])
	}
}
