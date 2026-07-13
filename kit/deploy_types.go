package kit

// deploy_types.go — kit-local aliases onto the spec-homed InstallPlan IR execution
// contract, so the host-side executor IMPLEMENTATIONS (ShellExecutor/SSHExecutor/
// NestedExecutor) + BuilderRun read their option structs unqualified. The host
// DeployExecutor interface is spec.DeployExecutor (NOT aliased here: kit already
// owns a DISTINCT DeployExecutor — the reverse-channel executor in walk.go — so the
// host-executor references spell it spec.DeployExecutor explicitly).

import "github.com/opencharly/sdk/spec"

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

// ValidateRecord is the egress-validation seam for ledger writes. The ledger
// (install_ledger.go) validates each record against its egress schema before
// writing; charly injects its ValidateEgressValue here at init (see
// charly/kit_ledger_aliases.go). Defaults to a no-op for standalone kit use.
var ValidateRecord = func(kind, label string, v any) error { return nil }
