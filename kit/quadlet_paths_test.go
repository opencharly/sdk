package kit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceName(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"fedora", "charly-fedora.service"},
		{"fedora-test", "charly-fedora-test.service"},
		{"ubuntu", "charly-ubuntu.service"},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got := ServiceName(tt.image)
			if got != tt.want {
				t.Errorf("ServiceName(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestServiceNameInstance(t *testing.T) {
	tests := []struct {
		image    string
		instance string
		want     string
	}{
		{"fedora", "", "charly-fedora.service"},
		{"githubrunner", "runner-1", "charly-githubrunner-runner-1.service"},
	}
	for _, tt := range tests {
		got := ServiceNameInstance(tt.image, tt.instance)
		if got != tt.want {
			t.Errorf("ServiceNameInstance(%q, %q) = %q, want %q", tt.image, tt.instance, got, tt.want)
		}
	}
}

func TestQuadletFilename(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"fedora", "charly-fedora.container"},
		{"fedora-test", "charly-fedora-test.container"},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got := QuadletFilename(tt.image)
			if got != tt.want {
				t.Errorf("QuadletFilename(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestQuadletFilenameInstance(t *testing.T) {
	tests := []struct {
		image    string
		instance string
		want     string
	}{
		{"fedora", "", "charly-fedora.container"},
		{"githubrunner", "runner-2", "charly-githubrunner-runner-2.container"},
	}
	for _, tt := range tests {
		got := QuadletFilenameInstance(tt.image, tt.instance)
		if got != tt.want {
			t.Errorf("QuadletFilenameInstance(%q, %q) = %q, want %q", tt.image, tt.instance, got, tt.want)
		}
	}
}

func TestQuadletDir(t *testing.T) {
	got, err := QuadletDir()
	if err != nil {
		t.Fatalf("QuadletDir() error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "containers", "systemd")
	if got != want {
		t.Errorf("QuadletDir() = %q, want %q", got, want)
	}
}

func TestSystemdUserDir(t *testing.T) {
	got, err := SystemdUserDir()
	if err != nil {
		t.Fatalf("SystemdUserDir() error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "systemd", "user")
	if got != want {
		t.Errorf("SystemdUserDir() = %q, want %q", got, want)
	}
}

func TestQuadletExists(t *testing.T) {
	tmpDir := t.TempDir()
	// Write a fake .container file
	systemdDir := filepath.Join(tmpDir, ".config", "containers", "systemd")
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemdDir, "charly-testimg.container"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Override HOME so QuadletDir resolves to our temp dir
	origHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome) //nolint:errcheck

	exists, err := QuadletExists("testimg")
	if err != nil {
		t.Fatalf("QuadletExists() error: %v", err)
	}
	if !exists {
		t.Error("expected QuadletExists to return true for existing file")
	}

	exists, err = QuadletExists("nonexistent")
	if err != nil {
		t.Fatalf("QuadletExists() error: %v", err)
	}
	if exists {
		t.Error("expected QuadletExists to return false for nonexistent file")
	}
}
