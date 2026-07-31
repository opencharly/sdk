package kit

import "github.com/opencharly/spec/checkhost"

// apk_path.go — ResolveApkPath + ResolveCommittedApk RELOCATED to the spec fabric slice
// github.com/opencharly/spec/checkhost (#55 CHECK-ENGINE cone Option A — the committed-APK
// candy-source anchoring the adb:/appium: check verbs + candy/plugin-adb's deploy:android collector
// share; a pure filesystem walk-up, homed in the check-host fabric slice so charly core reaches it
// importing zero kit). kit re-exports so existing kit.ResolveApkPath / kit.ResolveCommittedApk call
// sites (charly + candy/plugin-adb) are untouched.
var (
	ResolveApkPath      = checkhost.ResolveApkPath
	ResolveCommittedApk = checkhost.ResolveCommittedApk
)
