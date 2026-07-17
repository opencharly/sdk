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
	JumpSSH          = kit.JumpSSH
	JumpVirshConsole = kit.JumpVirshConsole
)

var (
	VmSshAlias = kit.VmSshAlias

	// AppendUnique — deploykit's own security.go merge logic + charly core's
	// (deploykit-consuming) call sites both use this unqualified/via `deploykit.`
	// (R3: security.go carried its own byte-identical copy of this helper under a
	// divergent name before this alias — see CHANGELOG).
	AppendUnique = kit.AppendUnique
)
