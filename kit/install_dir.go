package kit

// install_dir.go — RE-EXPORT shim. The atomic directory-install primitive (renameat2
// RENAME_EXCHANGE swap or plain rename) was RELOCATED to the spec/lock fabric slice
// (#55 coneB build-render cone, Class A — github.com/opencharly/spec/lock/install_dir_coneb.go).
// This file re-exports it so every existing kit.InstallDirAtomic call site (charly core's
// generate.go + build_stage_atomic_test.go, candy/plugin-build) is unchanged. New consumers
// reference spec/lock directly.

import "github.com/opencharly/spec/lock"

// InstallDirAtomic atomically installs the freshly-populated tmp directory as final.
var InstallDirAtomic = lock.InstallDirAtomic
