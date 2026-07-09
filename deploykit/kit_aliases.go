package deploykit

// kit_aliases.go — deploykit bindings onto the host-side executor IMPLEMENTATIONS
// in sdk/kit, so the deploy-tree walk + executor-derivation (deploy_tree.go) and the
// executor chain-builder (deploy_chain.go) construct + type-switch the concrete
// executors unqualified. deploykit imports kit (kit does NOT import deploykit — no
// cycle); the tree/chain are the "executor pick" orchestration the plan homes here.

import "github.com/opencharly/sdk/kit"

type (
	ShellExecutor  = kit.ShellExecutor
	SSHExecutor    = kit.SSHExecutor
	NestedExecutor = kit.NestedExecutor
	NestedJump     = kit.NestedJump
	JumpKind       = kit.JumpKind
)

const (
	VenueLocal = kit.VenueLocal

	JumpPodmanExec   = kit.JumpPodmanExec
	JumpDockerExec   = kit.JumpDockerExec
	JumpPodmanRun    = kit.JumpPodmanRun
	JumpDockerRun    = kit.JumpDockerRun
	JumpSSH          = kit.JumpSSH
	JumpVirshConsole = kit.JumpVirshConsole
)

var VmSshAlias = kit.VmSshAlias
