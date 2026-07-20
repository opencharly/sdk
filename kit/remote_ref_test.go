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
