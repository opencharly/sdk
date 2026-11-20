package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnvdDirAndFishUseCharly is the R7 control for the directory rename: the
// pre-change helpers used opencharly/env.d and opencharly.fish.
func TestEnvdDirAndFishUseCharly(t *testing.T) {
	home := t.TempDir()
	if got, want := EnvdDir(home), filepath.Join(home, ".config", "charly", "env.d"); got != want {
		t.Errorf("EnvdDir() = %q, want %q", got, want)
	}
	if got, want := EnvdFilePath(home, "example"), filepath.Join(home, ".config", "charly", "env.d", "example.env"); got != want {
		t.Errorf("EnvdFilePath() = %q, want %q", got, want)
	}
	if got, want := ShellInitFilePath(ShellFish, home), filepath.Join(home, ".config", "fish", "conf.d", "charly.fish"); got != want {
		t.Errorf("ShellInitFilePath(fish) = %q, want %q", got, want)
	}
}

// TestRemoveEnvdFile proves removal is silent-success both when the file
// exists and when it's already gone (double-remove). The file is created
// directly via EnvdFilePath + RenderEnvdBody — the same primitives the live
// WalkPlans write path uses.
func TestRemoveEnvdFile(t *testing.T) {
	home := t.TempDir()
	path := EnvdFilePath(home, "pre-commit")
	if err := os.MkdirAll(EnvdDir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	body := RenderEnvdBody("pre-commit", map[string]string{"K": "v"}, []string{"/bin"})
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
	if err := RemoveEnvdFile(home, "pre-commit"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after remove: %v", err)
	}
	// Remove again — should not error.
	if err := RemoveEnvdFile(home, "pre-commit"); err != nil {
		t.Errorf("double-remove errored: %v", err)
	}
}
