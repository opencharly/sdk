package deploykit

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
type DeployTreeVisitor func(path string, node *FleetNode, parentExec DeployExecutor) (childExec DeployExecutor, err error)

// WalkDeploymentTree performs a pre-order walk rooted at the given
// node, calling visit on each node. Dotted-path accumulation is
// handled internally: the root's `rootPath` argument seeds the
// identifier; children are rendered as `<parent>.<childKey>`.
//
// Errors short-circuit: as soon as any visit call returns a non-nil
// error, the walk stops and that error propagates.
func WalkDeploymentTree(rootPath string, root *FleetNode, parentExec DeployExecutor, visit DeployTreeVisitor) error {
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
// (specexec.AppendHopForFlatPath, spec/exec/deploy_chain.go) reads
// node.Descent.Transport (the plugin-declared venue), never a switch on the kind
// word. It serves BOTH deploy (pre-order WalkDeploymentTree above) and teardown
// (fleet del via resolveDelNode + the flat-path chain). The vm venue-hop helper
// the flat-path visitor calls (VmChildExecutor) and its SSHParamsForVm live in
// spec/exec (the floor primitive home, #55 K4); deploykit re-exports them from
// deploy_chain.go for its plugin-side callers.

// ClassifyTarget normalizes the Target field for dispatch. Empty Target
// falls back to "pod" (the default for named deploys); otherwise Target is
// the canonical source of truth (pod|vm|kubernetes|local|android — set from the
// node-form kind by fleetTargetForDisc; no name-prefix heuristic).
func ClassifyTarget(node *FleetNode) string {
	if node == nil || node.Target == "" {
		return "pod"
	}
	return node.Target
}
