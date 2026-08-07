package deploykit

import (
	"github.com/opencharly/sdk/kit"
	specexec "github.com/opencharly/spec/exec"
)

// deploy_chain.go — the executor-CHAIN constructors moved to spec/exec (the floor primitive
// home, beside the ShellExecutor/SSHExecutor/NestedExecutor they build — #55 K4); deploykit
// RE-EXPORTS them here for its plugin-side callers (candy/plugin-fleet, plugin-check,
// plugin-status, plugin-vm) so those `deploykit.<Fn>` references compile unchanged. charly core
// calls specexec.<Fn> directly (no deploykit import for the chain). ContainerChain STAYS — it
// delegates to kit.ContainerChainFromDescriptor (kit-coupled, not a spec/exec pure constructor).
var (
	// RootExecutorForDeployNode selects the ROOT DeployExecutor for a `target: local` node from
	// its `host:` field (ShellExecutor for host:local, SSHExecutor for host:<remote>).
	RootExecutorForDeployNode = specexec.RootExecutorForDeployNode
	// ResolveDeployChain walks a dotted deployment path through the merged tree and returns the
	// leaf node + a composed DeployExecutor chain reaching it.
	ResolveDeployChain = specexec.ResolveDeployChain
	// VmChildExecutor wraps a parent executor with an SSH jump into a VM child node.
	VmChildExecutor = specexec.VmChildExecutor
	// SSHParamsForVm builds an SSHExecutor pointing at a VM's managed ssh-config alias.
	SSHParamsForVm = specexec.SSHParamsForVm
)

// ContainerChain returns a one-hop chain that exec's into a single named
// running container (`<engine> exec <name> bash`). Convenience for the
// simple `charly check live <name>` path where there is no nested dotted path to
// walk — equivalent to ResolveDeployChain on a single-segment dotted
// path that resolves to a pod node, but skips the tree lookup.
//
// Delegates to kit.ContainerChainFromDescriptor (K1-unblock W3 Unit B, R3 — one construction, not
// two): that function is the SAME shape, promoted to sdk/kit so kit.VenueFromDescriptor's new
// "container" VenueDescriptor kind re-materializes byte-identically to this constructor, letting
// a plugin-constructed ContainerChain venue round-trip over InvokeProvider's VenueDescriptor seam.
func ContainerChain(engine, containerName string) DeployExecutor {
	return kit.ContainerChainFromDescriptor(engine, containerName)
}
