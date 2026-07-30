package kit

import "github.com/opencharly/spec/lock"

// filelock.go — re-export of the advisory-flock primitive, RELOCATED to the spec/lock fabric
// slice (#55 fabric-primitive extraction). A file lock is a host primitive a plugin cannot hold,
// so it homes in spec/lock (carrying syscall in its own slice, Rule 2); kit re-exports the
// symbols here so the compiled-in candy/plugin-preempt and every existing kit.AcquireFileLock /
// kit.ErrLockBusy / … call site are untouched. New consumers should import spec/lock directly.
var (
	ErrLockBusy              = lock.ErrLockBusy
	AcquireFileLock          = lock.AcquireFileLock
	ImageBuildLockPath       = lock.ImageBuildLockPath
	AcquireImageBuildLock    = lock.AcquireImageBuildLock
	AcquireLocalPkgBuildLock = lock.AcquireLocalPkgBuildLock
)
