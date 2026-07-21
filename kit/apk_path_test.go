package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveApkPath checks committed-APK path resolution: absolute verbatim, candy-relative
// when present, project-root-relative via walk-up (the @github fetched-candy case), and a HARD
// ERROR when nothing resolves (no silent cwd-relative pass-through). Relocated from
// charly/apk_format_test.go (FINAL/K5 unit 6a) alongside ResolveApkPath itself.
func TestResolveApkPath(t *testing.T) {
	if got, err := ResolveApkPath("/abs/x.apk", "/layers/foo"); err != nil || got != "/abs/x.apk" {
		t.Errorf("absolute path = (%q,%v), want (/abs/x.apk,nil)", got, err)
	}
	// No anchor resolves (candyDir + ancestors lack the file) → HARD ERROR.
	if _, err := ResolveApkPath("tests/data/x.apk", "/nonexistent-layer-dir"); err == nil {
		t.Error("missing file under candyDir must error, got nil")
	}
	// No candy dir at all → HARD ERROR (cannot anchor a relative ref).
	if _, err := ResolveApkPath("tests/data/x.apk", ""); err == nil {
		t.Error("empty candyDir for a relative ref must error, got nil")
	}

	// Fetched-candy layout: <repo>/tests/data/x.apk exists, candyDir is
	// <repo>/candy/android-apidemos (the file is NOT under candyDir). The walk-up
	// must resolve the project-root-relative ref to <repo>/tests/data/x.apk.
	repo := t.TempDir()
	candyDir := filepath.Join(repo, "candy", "android-apidemos")
	if err := os.MkdirAll(candyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apk := filepath.Join(repo, "tests", "data", "x.apk")
	if err := os.MkdirAll(filepath.Dir(apk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apk, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveApkPath("tests/data/x.apk", candyDir); err != nil || got != apk {
		t.Errorf("project-root walk-up = (%q,%v), want (%q,nil)", got, err, apk)
	}

	// Candy-relative takes priority (closest anchor wins) when the file sits directly under
	// candyDir.
	localApk := filepath.Join(candyDir, "local.apk")
	if err := os.WriteFile(localApk, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveApkPath("local.apk", candyDir); err != nil || got != localApk {
		t.Errorf("candy-relative = (%q,%v), want (%q,nil)", got, err, localApk)
	}
}
