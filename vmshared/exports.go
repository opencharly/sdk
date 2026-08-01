package vmshared

import "github.com/opencharly/spec/hostenv"

// exports.go — the exported surface of this package for its two consumers
// (the charly core in charly/, and the out-of-process VM plugin in
// candy/plugin-vm/). Each entry re-exports an internal helper under an
// exported name so the consumers can reach it across the package boundary;
// the internal (unexported) definitions and their intra-package call sites
// are left untouched. Symbols used ONLY within this package are not listed.

// Re-exported functions.
var (
	BoolPtrDefaultTrue       = boolPtrDefaultTrue
	BoolPtrToYesNo           = boolPtrToYesNo
	BoolPtrTrue              = boolPtrTrue
	ComposePackages          = composePackages
	ComposeRunCmd            = composeRunCmd
	ComposeUsers             = composeUsers
	DefaultMachineForArch    = defaultMachineForArch
	FormatForDistroID        = hostenv.FormatForDistroID
	LoadRegistry             = loadRegistry
	OpenOutputPath           = openOutputPath
	OvmfCandidatesForDistro  = ovmfCandidatesForDistro
	ParseGlibcVersion        = hostenv.ParseGlibcVersion
	RegistryPath             = registryPath
	ResolveCloudInitSSHUser  = resolveCloudInitSSHUser
	ResolveCPUDefaults       = resolveCPUDefaults
	SaveRegistry             = saveRegistry
	SnapshotExternalDiskPath = snapshotExternalDiskPath
	SnapshotsDir             = snapshotsDir
	SplitOsReleaseLine       = hostenv.SplitOsReleaseLine
	SplitPortForward         = splitPortForward
	VmDiskPath               = vmDiskPath
	WriterForPath            = writerForPath
)
