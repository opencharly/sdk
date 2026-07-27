package deploykit

import "testing"

// Relocated from charly/volumes_test.go in the core-min wave-3 build-cluster split, alongside
// the expandVolumeHome + sortVolumeMounts helpers they cover (volume_collect.go).

func TestExpandVolumeHome(t *testing.T) {
	tests := []struct {
		path string
		home string
		want string
	}{
		{"~/.openclaw", "/home/user", "/home/user/.openclaw"},
		{"~", "/home/user", "/home/user"},
		{"$HOME/.config", "/home/user", "/home/user/.config"},
		{"/absolute/path", "/home/user", "/absolute/path"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := expandVolumeHome(tt.path, tt.home)
			if got != tt.want {
				t.Errorf("expandVolumeHome(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
			}
		})
	}
}

func TestSortVolumeMounts(t *testing.T) {
	mounts := []VolumeMount{
		{VolumeName: "charly-app-z", ContainerPath: "/z"},
		{VolumeName: "charly-app-a", ContainerPath: "/a"},
		{VolumeName: "charly-app-m", ContainerPath: "/m"},
	}
	sortVolumeMounts(mounts)
	if mounts[0].VolumeName != "charly-app-a" || mounts[1].VolumeName != "charly-app-m" || mounts[2].VolumeName != "charly-app-z" {
		t.Errorf("sortVolumeMounts() result: %v", mounts)
	}
}
