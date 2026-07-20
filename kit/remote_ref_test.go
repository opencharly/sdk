package kit

import "testing"

func TestStripURLScheme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/org/repo/image", "github.com/org/repo/image"},
		{"http://github.com/org/repo/image", "github.com/org/repo/image"},
		{"github.com/org/repo/image", "github.com/org/repo/image"},
		{"@github.com/org/repo/box:v1.0.0", "@github.com/org/repo/box:v1.0.0"},
		{"myimage", "myimage"},
	}

	for _, tt := range tests {
		got := StripURLScheme(tt.input)
		if got != tt.want {
			t.Errorf("StripURLScheme(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestResolveBoxName moved from charly/remote_image_test.go (CHECK-wave container-resolve
// dedup) — resolveBoxName dissolved into this package's own ResolveBoxName.
func TestResolveBoxName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"myapp", "myapp"},
		{"@github.com/org/repo/myapp:v1.0.0", "myapp"},
		{"simple-image", "simple-image"},
	}

	for _, tt := range tests {
		got := ResolveBoxName(tt.input)
		if got != tt.want {
			t.Errorf("ResolveBoxName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
