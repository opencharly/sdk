package deploykit

import (
	"sort"

	"github.com/opencharly/sdk/vmshared"
)

// DeployTreePhase indicates which lifecycle phase the walker is in.
// Pre-order for add; teardown walks the flat-path chain (deploy_chain.go).
type DeployTreePhase int

const (
	DeployTreePhaseAdd DeployTreePhase = iota
	DeployTreePhaseDel
)

// DeployTreeVisitor is invoked once per node in the walk. It receives
// the node's dotted path, the node itself, and the parent executor
// (nil at the root). The return value is the DeployExecutor that this
// node's CHILDREN should use as their parent. A host-target node
// returns the same executor it was given (candies applied in-place on
// the parent venue); a container or vm node returns a NestedExecutor
// that drills into the newly-created environment.
//
// Returning (nil, nil) for a node with children is an error — it
// means "cannot compute child executor", which the walker surfaces
// with the offending path.
type DeployTreeVisitor func(path string, node *BundleNode, parentExec DeployExecutor) (childExec DeployExecutor, err error)

// WalkDeploymentTree performs a pre-order walk rooted at the given
// node, calling visit on each node. Dotted-path accumulation is
// handled internally: the root's `rootPath` argument seeds the
// identifier; children are rendered as `<parent>.<childKey>`.
//
// Errors short-circuit: as soon as any visit call returns a non-nil
// error, the walk stops and that error propagates.
func WalkDeploymentTree(rootPath string, root *BundleNode, parentExec DeployExecutor, visit DeployTreeVisitor) error {
	if root == nil {
		return nil
	}
	thisExec, err := visit(rootPath, root, parentExec)
	if err != nil {
		return err
	}
	if !root.HasChildren() {
		return nil
	}
	for _, k := range SortedNestedKeys(root.Children) {
		child := root.Children[k]
		childPath := k
		if rootPath != "" {
			childPath = rootPath + "." + k
		}
		if err := WalkDeploymentTree(childPath, child, thisExec, visit); err != nil {
			return err
		}
	}
	return nil
}

// Child-executor derivation is trait-based: the flat-path executor chain
// (AppendHopForFlatPath, deploy_chain.go) reads node.Descent.Transport (the
// plugin-declared venue), never a switch on the kind word. It serves BOTH
// deploy (pre-order WalkDeploymentTree above) and teardown (bundle del via
// resolveDelNode + the flat-path chain). VmChildExecutor below is the one
// venue-hop helper the flat-path visitor still calls, for a vm venue.

// VmChildExecutor wraps parentExec with an SSH jump into the VM
// represented by this node. At the root (parentExec == nil or
// ShellExecutor), the child gets a plain SSHExecutor — no
// nesting overhead for the common case of a VM on localhost.
//
// The SSH alias keys off the per-deploy DOMAIN IDENTITY
// (charly-<VmDomainIdentity(deployName)>), NOT node.From (the shared kind:vm
// entity) — `charly vm create <entity> --domain <deploy>` writes the managed
// stanza under `charly-<deploy>` (P33). Several beds may share one entity via
// `from:`, so an entity-keyed alias collides them on ONE stanza (the R10 defect
// where sibling beds both derived `charly-eval-vm`); keying by the deploy makes
// the alias distinct per bed and matches the stanza vm create actually wrote. A
// direct create (deploy == entity) resolves to `charly-<entity>` naturally, and
// VmDomainIdentity flattens a dotted member path consistently with the domain the
// lifecycle named (bundle_members.go's `vmDomainIdentity(memberKey)`).
func VmChildExecutor(parentExec DeployExecutor, deployName string) (DeployExecutor, error) {
	ssh := SSHParamsForVm(vmshared.VmDomainIdentity(deployName))
	// If parent is localhost-equivalent, use a direct SSHExecutor —
	// no need to hop through a trivial wrapper.
	if parentExec == nil {
		return ssh, nil
	}
	if _, isLocal := parentExec.(ShellExecutor); isLocal {
		return ssh, nil
	}
	// Nested VM (inside a container, or inside another VM): compose
	// using the same alias as the JumpSSH target — ssh-config supplies
	// User/Port/IdentityFile.
	return &NestedExecutor{
		Parent: parentExec,
		Jump: NestedJump{
			Kind:   JumpSSH,
			Target: ssh.Host,
		},
	}, nil
}

// SSHParamsForVm returns an SSHExecutor pointing at the VM's managed
// ssh-config alias (charly-<domainID>) — the caller passes the per-deploy
// DOMAIN IDENTITY (VmDomainIdentity of the deploy), NOT the shared kind:vm
// entity (P33). All connection details — User, Port, IdentityFile, host-key
// checking — live in the Host stanza that `charly vm create` / `charly bundle
// add` published into ~/.config/charly/ssh_config; ssh(1) reads them from there.
// Our SSHExecutor needs only the alias as Host.
func SSHParamsForVm(domainID string) *SSHExecutor {
	return &SSHExecutor{
		Host:           VmSshAlias(domainID),
		ConnectTimeout: 10,
	}
}

// ClassifyTarget normalizes the Target field for dispatch. Empty Target
// falls back to "pod" (the default for named deploys); otherwise Target is
// the canonical source of truth (pod|vm|k8s|local|android — set from the
// node-form kind by bundleTargetForDisc; no name-prefix heuristic).
func ClassifyTarget(node *BundleNode) string {
	if node == nil || node.Target == "" {
		return "pod"
	}
	return node.Target
}

// stampBundleDescents stamps every deploy node's venue-hop descent-descriptor via
// the shared kit mapping (the descent de-type, Cutover H). Applied uniformly by the
// loader AFTER all structural kinds have folded into uf.Bundle — so every
// substrate/group/custom structural kind gets a descriptor without each plugin
// remembering to stamp — and the kernel's deploy chain (AppendHopForFlatPath)
// descends by TRANSPORT, never by a kind-word switch. The word→transport MAPPING
// lives in sdk/kit, never the kernel; StampDescent recurses the nested/peer subtree.

func SortedNestedKeys(children map[string]*BundleNode) []string {
	out := make([]string, 0, len(children))
	for k := range children {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
