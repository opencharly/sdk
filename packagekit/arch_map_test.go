package packagekit

import "testing"

func TestArchMap(t *testing.T) {
	cases := []struct {
		format, goarch, want string
	}{
		{"deb", "amd64", "amd64"},
		{"deb", "arm64", "arm64"},
		{"rpm", "amd64", "x86_64"},
		{"rpm", "arm64", "aarch64"},
		{"apk", "amd64", "x86_64"},
		{"apk", "arm64", "aarch64"},
		{"archlinux", "amd64", "x86_64"},
		{"archlinux", "arm64", "aarch64"},
		{"ipk", "amd64", "x86_64"},
		{"ipk", "arm64", "arm64"},
		{"msix", "amd64", "x64"},
		{"msix", "arm64", "arm64"},
	}
	for _, c := range cases {
		if got := ArchMap(c.format, c.goarch); got != c.want {
			t.Errorf("ArchMap(%q, %q) = %q, want %q", c.format, c.goarch, got, c.want)
		}
	}
}
