package kit

// exec_aliases.go — kit re-exports the host/guest EXECUTOR slice, relocated to the
// fabric module github.com/opencharly/spec/exec (#55 spec/exec, the granular
// executor slice: os/exec + spec/proc only, NO x/crypto/ssh). The concrete
// executors (ShellExecutor/SSHExecutor/NestedExecutor), their SSH readiness
// waits, VenueDescriptor re-materialization, the podman builder-run engine, the
// container-infra-failure classifier, and the charly-into-venue delivery all live
// in spec/exec now; these aliases keep every existing kit.X caller (charly core,
// sdk/deploykit, the ~85 plugins) compiling unchanged (R3, single source — the
// buildkit/vmshared re-export pattern).

import (
	"github.com/opencharly/spec/exec"
	"github.com/opencharly/spec/poll"
)

type (
	ShellExecutor         = exec.ShellExecutor
	SSHExecutor           = exec.SSHExecutor
	NestedExecutor        = exec.NestedExecutor
	NestedJump            = exec.NestedJump
	JumpKind              = exec.JumpKind
	PollCond              = poll.PollCondition
	PollFunc              = exec.PollFunc
	SSHArgs               = exec.SSHArgs
	CharlyInstallStrategy = exec.CharlyInstallStrategy
)

const (
	VenueLocal              = exec.VenueLocal
	SignalKillErrMarker     = exec.SignalKillErrMarker
	PodmanInfraExitCode     = exec.PodmanInfraExitCode
	ContainerInfraErrMarker = exec.ContainerInfraErrMarker

	// JumpContainerExec is the ONE container-exec jump arm; the ENGINE is DATA
	// (NestedJump.Engine, a podman/docker/nerdctl word), so there is no per-engine
	// arm (the former JumpPodmanExec/JumpDockerExec are removed — a new engine sets
	// NestedJump.Engine, it does not add a JumpKind).
	JumpContainerExec = exec.JumpContainerExec
	JumpSSH           = exec.JumpSSH
	JumpVirshConsole  = exec.JumpVirshConsole

	CharlyInstallAuto = exec.CharlyInstallAuto
	CharlyInstallScp  = exec.CharlyInstallScp
	CharlyInstallSkip = exec.CharlyInstallSkip
)

var (
	RunCaptureCmd       = exec.RunCaptureCmd
	NestedContainerName = exec.NestedContainerName

	WaitForSSH         = exec.WaitForSSH
	WaitForCloudInit   = exec.WaitForCloudInit
	WaitForPackageLock = exec.WaitForPackageLock

	VenueFromDescriptor          = exec.VenueFromDescriptor
	ContainerChainFromDescriptor = exec.ContainerChainFromDescriptor
	DescriptorFromExecutor       = exec.DescriptorFromExecutor

	ResolveCharlyInstallStrategy = exec.ResolveCharlyInstallStrategy
	HostCharlyIsNewer            = exec.HostCharlyIsNewer
	EnsureCharlyInDeployVenue    = exec.EnsureCharlyInDeployVenue
	EnsureCharlyInGuest          = exec.EnsureCharlyInGuest

	BuilderRun          = exec.BuilderRun
	BuildBuilderRunArgs = exec.BuildBuilderRunArgs
	UserScopeBindMounts = exec.UserScopeBindMounts
	UserScopeEnv        = exec.UserScopeEnv

	ClassifyContainerInfraFailure = exec.ClassifyContainerInfraFailure
	IsContainerInfraResult        = exec.IsContainerInfraResult
	ContainerInfraError           = exec.ContainerInfraError
)
