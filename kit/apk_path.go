package kit

import (
	"fmt"
	"os"
	"path/filepath"
)

// apk_path.go — the shared committed-APK path resolver (FINAL/K5 unit 6a, relocated from
// charly/android_deploy_preresolve.go's resolveApkPath). Used by BOTH charly core's
// resolveCheckApk (the adb:/appium: check-verb fixture anchor, checkrun_charly_verbs.go) and
// candy/plugin-adb's deploy:android install-spec collector (preresolve.go) — one shared
// implementation, R3, since neither context is LoadUnified-coupled: both already hold the
// resolved candy source DIRECTORY (from the host's live candy scan, or the compiled InstallPlan's
// ApkInstallStep.CandyDir) and only need the pure filesystem walk-up.

// ResolveApkPath resolves a committed-APK reference against the candy's SOURCE tree. Absolute
// paths are used verbatim; a relative path anchors candy-dir-relative first, then each ancestor up
// to the candy's project / repo root (first existing match wins). This resolves a path like
// `tests/data/ApiDemos-debug.apk` identically whether the candy is LOCAL (candyDir under the
// consuming project root) or fetched via @github (candyDir under the cloned-repo cache, where a
// project-root-relative file lives at <repo-root>/tests/data/... several levels above candyDir).
//
// It FAILS HARD when a relative ref has no candy dir to anchor against, or when the file is not
// found anywhere up the tree — the caller surfaces that, never silently passing an unresolvable
// path downstream.
func ResolveApkPath(ref, candyDir string) (string, error) {
	if filepath.IsAbs(ref) {
		return ref, nil
	}
	if candyDir == "" {
		return "", fmt.Errorf("cannot resolve relative committed APK %q: no candy source dir to anchor against", ref)
	}
	for dir := candyDir; ; {
		cand := filepath.Join(dir, ref)
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("committed APK %q not found under candy source tree %q (searched every ancestor up to the filesystem root)", ref, candyDir)
		}
		dir = parent
	}
}
