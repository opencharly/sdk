// Package deploykit re-exports the InstallPlan step VOCABULARY that now lives in
// spec (#55 step-4): the 13 concrete InstallStep implementations + their field-structs
// + the pure classification helpers. They RELOCATED to github.com/opencharly/spec/spec
// (install_step_vocab.go) — in-proc IR, hand-written beside the InstallStep interface +
// InstallPlan container it belongs to (its wire form is the CUE-sourced InstallStepView).
// The thin aliases below let deploykit's own code + out-of-tree plugins read unchanged;
// deploykit imports spec.
package deploykit

import "github.com/opencharly/spec/spec"

type (
	Phase          = spec.Phase
	Scope          = spec.Scope
	Venue          = spec.Venue
	Gate           = spec.Gate
	StepKind       = spec.StepKind
	ReverseOp      = spec.ReverseOp
	ReverseOpKind  = spec.ReverseOpKind
	ApkPackageSpec = spec.ApkPackageSpec
	CacheMountDef  = spec.CacheMount
	BuilderDef     = spec.BuilderDef
	Op             = spec.Op
	LocalPkgDef    = spec.LocalPkg
	InstallStep    = spec.InstallStep
	BundleNode     = spec.BundleNode
	EmitOpts       = spec.EmitOpts
	DeployExecutor = spec.DeployExecutor
	BuilderRunOpts = spec.BuilderRunOpts
	InstallPlan    = spec.InstallPlan
	StepBatch      = spec.StepBatch
	DeployTarget   = spec.DeployTarget

	// The step VOCABULARY (relocated to spec, #55 step-4 — in-proc IR).
	RepoSpec            = spec.RepoSpec
	CacheMountSpec      = spec.CacheMountSpec
	ArtifactRef         = spec.ArtifactRef
	SystemPackagesStep  = spec.SystemPackagesStep
	BuilderStep         = spec.BuilderStep
	OpStep              = spec.OpStep
	FileStep            = spec.FileStep
	ServicePackagedStep = spec.ServicePackagedStep
	ServiceCustomStep   = spec.ServiceCustomStep
	ShellHookStep       = spec.ShellHookStep
	ShellSnippetStep    = spec.ShellSnippetStep
	RepoChangeStep      = spec.RepoChangeStep
	ApkInstallStep      = spec.ApkInstallStep
	LocalPkgInstallStep = spec.LocalPkgInstallStep
	RebootStep          = spec.RebootStep
	ExternalPluginStep  = spec.ExternalPluginStep
	ExternalStep        = spec.ExternalStep
)

const (
	ScopeSystem      = spec.ScopeSystem
	ScopeUser        = spec.ScopeUser
	ScopeUserProfile = spec.ScopeUserProfile

	PhasePrepare = spec.PhasePrepare
	PhaseInstall = spec.PhaseInstall
	PhaseCleanup = spec.PhaseCleanup

	VenueHostNative       = spec.VenueHostNative
	VenueContainerBuilder = spec.VenueContainerBuilder
	VenueSkip             = spec.VenueSkip

	GateNone             = spec.GateNone
	GateAllowRepoChanges = spec.GateAllowRepoChanges
	GateAllowRootTasks   = spec.GateAllowRootTasks
	GateWithServices     = spec.GateWithServices

	StepKindSystemPackages  = spec.StepKindSystemPackages
	StepKindBuilder         = spec.StepKindBuilder
	StepKindOp              = spec.StepKindOp
	StepKindFile            = spec.StepKindFile
	StepKindServicePackaged = spec.StepKindServicePackaged
	StepKindServiceCustom   = spec.StepKindServiceCustom
	StepKindShellHook       = spec.StepKindShellHook
	StepKindShellSnippet    = spec.StepKindShellSnippet
	StepKindRepoChange      = spec.StepKindRepoChange
	StepKindApkInstall      = spec.StepKindApkInstall
	StepKindLocalPkgInstall = spec.StepKindLocalPkgInstall
	StepKindReboot          = spec.StepKindReboot
	StepKindExternalPlugin  = spec.StepKindExternalPlugin

	ExternalStepKindPrefix = spec.ExternalStepKindPrefix

	ReverseOpCoprDisable    = spec.ReverseOpCoprDisable
	ReverseOpPackageRemove  = spec.ReverseOpPackageRemove
	ReverseOpRemoveDropin   = spec.ReverseOpRemoveDropin
	ReverseOpRemoveEnvdFile = spec.ReverseOpRemoveEnvdFile
	ReverseOpRemoveManaged  = spec.ReverseOpRemoveManaged
	ReverseOpRemoveRepoFile = spec.ReverseOpRemoveRepoFile
	ReverseOpRestoreEnabled = spec.ReverseOpRestoreEnabled
	ReverseOpRmFileSystem   = spec.ReverseOpRmFileSystem
	ReverseOpRmFileUser     = spec.ReverseOpRmFileUser
	ReverseOpServiceDisable = spec.ReverseOpServiceDisable
	ReverseOpServiceRemove  = spec.ReverseOpServiceRemove
)

// The fixed step-kind vocabulary + the pure classification helpers relocated to spec.
var AllStepKinds = spec.AllStepKinds

var (
	OpStepScope        = spec.OpStepScope
	PathIsSystemScoped = spec.PathIsSystemScoped
	IsExternalStepKind = spec.IsExternalStepKind
)
