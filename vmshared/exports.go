package vmshared

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
	CurrentUsername          = currentUsername
	DefaultMachineForArch    = defaultMachineForArch
	FormatForDistroID        = formatForDistroID
	LoadRegistry             = loadRegistry
	OpenOutputPath           = openOutputPath
	OvmfCandidatesForDistro  = ovmfCandidatesForDistro
	ParseGlibcVersion        = parseGlibcVersion
	RegistryPath             = registryPath
	ResolveCloudInitSSHUser  = resolveCloudInitSSHUser
	ResolveCPUDefaults       = resolveCPUDefaults
	SaveRegistry             = saveRegistry
	SnapshotExternalDiskPath = snapshotExternalDiskPath
	SnapshotsDir             = snapshotsDir
	SplitOsReleaseLine       = splitOsReleaseLine
	SplitPortForward         = splitPortForward
	VmDiskPath               = vmDiskPath
	WriterForPath            = writerForPath
)
