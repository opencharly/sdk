package kit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// TestResolveCommittedApk covers the check-verb origin/candyDirs lookup layer atop
// ResolveApkPath — relocated from charly's TestResolveCheckApk (CHECK-cone move) alongside
// ResolveCommittedApk itself. Anchors a relative committed-APK ref against the AUTHORING
// candy's source dir (candyDirs[origin-key]) and FAILS HARD on every condition where it
// cannot — a non-candy origin, an absent candyDirs entry, or a missing file.
func TestResolveCommittedApk(t *testing.T) {
	repo := t.TempDir()
	apk := filepath.Join(repo, "tests", "data", "x.apk") // project-root fixture
	if err := os.MkdirAll(filepath.Dir(apk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apk, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	authorDir := filepath.Join(repo, "candy", "android-emulator-layer")
	siblingDir := filepath.Join(repo, "candy", "sshd")
	for _, d := range []string{authorDir, siblingDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// LOCAL candy: map key == bare name. Origin "candy:<name>" → resolves.
	dirs := map[string]string{"android-emulator-layer": authorDir, "sshd": siblingDir}
	if got, err := ResolveCommittedApk("./tests/data/x.apk", "candy:android-emulator-layer", dirs, nil); err != nil || got != apk {
		t.Errorf("local-candy resolve = (%q,%v), want (%q,nil)", got, err, apk)
	}
	// FETCHED candy: map key == bare @github ref, and the step Origin is stamped with that
	// same ref. candyDirs[ref] must match.
	const ref = "github.com/owner/repo/candy/android-emulator-layer"
	dirsRemote := map[string]string{ref: authorDir}
	if got, err := ResolveCommittedApk("./tests/data/x.apk", "candy:"+ref, dirsRemote, nil); err != nil || got != apk {
		t.Errorf("fetched-candy (ref-keyed) resolve = (%q,%v), want (%q,nil)", got, err, apk)
	}
	// Authoring candy NOT in candyDirs → HARD ERROR (no fallback to a sibling).
	dirsSibling := map[string]string{"sshd": siblingDir}
	if _, err := ResolveCommittedApk("./tests/data/x.apk", "candy:android-emulator-layer", dirsSibling, nil); err == nil {
		t.Error("unknown candy must error, got nil")
	}
	// A scan error surfaces as the root cause (not a misleading not-found).
	if _, err := ResolveCommittedApk("./tests/data/x.apk", "candy:android-emulator-layer", dirsSibling, errors.New("boom")); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("scan-error path = %v, want error mentioning the scan failure", err)
	}
	// Non-candy origin (the step's candy Origin was lost) → HARD ERROR.
	if _, err := ResolveCommittedApk("./tests/data/x.apk", "box:android-emulator", dirsSibling, nil); err == nil {
		t.Error("non-candy origin must error, got nil")
	}
	// Absolute passes through (no anchoring needed).
	if got, err := ResolveCommittedApk("/abs/y.apk", "candy:foo", dirsSibling, nil); err != nil || got != "/abs/y.apk" {
		t.Errorf("absolute = (%q,%v), want (/abs/y.apk,nil)", got, err)
	}
}
