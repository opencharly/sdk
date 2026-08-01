package kit

// deploy_types.go — kit-local aliases onto the spec-homed InstallPlan IR execution
// contract, so the host-side executor IMPLEMENTATIONS (ShellExecutor/SSHExecutor/
// NestedExecutor) + BuilderRun read their option structs unqualified. The host
// DeployExecutor interface is spec.DeployExecutor (NOT aliased here: kit already
// owns a DISTINCT DeployExecutor — the reverse-channel executor in walk.go — so the
// host-executor references spell it spec.DeployExecutor explicitly).

import "github.com/opencharly/spec/spec"

type (
	EmitOpts       = spec.EmitOpts
	BuilderRunOpts = spec.BuilderRunOpts
	ReverseOp      = spec.ReverseOp
	ReverseOpKind  = spec.ReverseOpKind
	StepKind       = spec.StepKind
	Scope          = spec.Scope
	Venue          = spec.Venue
	VmSource       = spec.VmSource
)

const (
	ScopeSystem      = spec.ScopeSystem
	ScopeUser        = spec.ScopeUser
	ScopeUserProfile = spec.ScopeUserProfile

	ReverseOpPackageRemove  = spec.ReverseOpPackageRemove
	ReverseOpCargoUninstall = spec.ReverseOpCargoUninstall
	ReverseOpNpmUninstallG  = spec.ReverseOpNpmUninstallG
	ReverseOpPixiEnvRemove  = spec.ReverseOpPixiEnvRemove
	ReverseOpRmFileSystem   = spec.ReverseOpRmFileSystem
	ReverseOpRmFileUser     = spec.ReverseOpRmFileUser
	ReverseOpRmDirRecursive = spec.ReverseOpRmDirRecursive
	ReverseOpServiceDisable = spec.ReverseOpServiceDisable
	ReverseOpServiceRemove  = spec.ReverseOpServiceRemove
	ReverseOpRemoveDropin   = spec.ReverseOpRemoveDropin
	ReverseOpRestoreEnabled = spec.ReverseOpRestoreEnabled
	ReverseOpRemoveManaged  = spec.ReverseOpRemoveManaged
	ReverseOpRemoveEnvdFile = spec.ReverseOpRemoveEnvdFile
	ReverseOpRemoveRepoFile = spec.ReverseOpRemoveRepoFile
	ReverseOpCoprDisable    = spec.ReverseOpCoprDisable
	ReverseOpPluginScript   = spec.ReverseOpPluginScript
)

// The ValidateRecord egress-validation seam (for install-ledger writes) now lives in
// spec (spec.ValidateRecord, #55 import-purity cone-render) so charly injects + the
// ledger reads ONE canonical var. install_ledger.go calls spec.ValidateRecord directly
// — a seam var cannot be a kit-side alias (an alias copies the no-op default at init,
// so charly's later injection would never reach the ledger).
