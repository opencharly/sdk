package loaderkit

// bundle_load.go — the PURE, registry-free LOAD-half of the bundle passes (K1-LOADER
// RELOCATION, moved from charly/node_bundle_venue.go + charly/bundle_members.go). These are
// the LoadSeams.FlattenBundleVenues / FoldMembers steps plus the two shared sort helpers they
// (and the DEPLOY-half that STAYS core — bringUpMembers/tearDownMembers) rely on. Every function
// here operates ONLY on the already-materialized *spec.UnifiedFile / spec.BundleNode maps with zero
// provider-registry or host coupling (boundary law clause M: a kind-blind mechanism consuming an
// envelope), so it runs identically host-side OR plugin-side — the property that lets
// loaderkit.LoadUnified wire these seams directly without a reverse-channel hop. Behaviour is
// byte-identical to the former charly copies.
//
// The DEPLOY-half — bringUpMembers / tearDownMembers / isPodMember / isVmMember / withMemberTag —
// STAYS host-resident (charly/bundle_members.go) per the lead's U1 SPLIT ruling: it shells out via
// proc.RunCharlySubcommand + reads the live provider registry (nodeTraits), so it is NOT
// registry-free and does NOT belong here.

import (
	"fmt"
	"sort"

	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/spec/spec"
)

// SortedDeployKeys returns a Bundle map's keys in deterministic (name) order. A generic
// Bundle-map helper with no kind-specific logic, shared by FoldMembers / FlattenBundleVenues /
// VenueIsAgentProvisioned here and by charly's DEPLOY-half owner-walk (R3, one shared abstraction).
func SortedDeployKeys(m map[string]spec.BundleNode) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SortedMemberKeys returns the member keys of a node in deterministic order.
func SortedMemberKeys(members map[string]*spec.BundleNode) []string {
	if len(members) == 0 {
		return nil
	}
	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// -----------------------------------------------------------------------------
// venue-from-position for bundle plan steps.
//
// In the unified node-form model a step's EXECUTION VENUE comes ENTIRELY from its POSITION in the
// bundle tree — there is no authored `on:`/`pod:` override (both retired). FlattenBundleVenues runs
// at load time, AFTER the tree is built and BEFORE FoldMembers + validation, and:
//
//   1. stamps every step's `venue` (Op.Venue) from its tree position:
//        - a step directly under a WORKLOAD root R          → "R"
//        - a step under a sibling MEMBER M                  → "M"            (bare)
//        - a step under a NESTED child C of parent path P   → "P.C"         (dotted)
//   2. HOISTS every member/child step into the ROOT bundle's flat Plan (and clears the
//      member/child Plan), because both runner entry points read the root node.Plan.
//
// A direct step under a pure GROUP root (no workload container) is a hard error — a group has no
// venue of its own; place the step under a member.
// -----------------------------------------------------------------------------

// FlattenBundleVenues stamps venue + hoists plan steps for every top-level bundle in uf.
// Idempotent on an already-flattened tree (members/children have empty Plan after the first pass,
// so re-running hoists nothing). Must run before FoldMembers (which promotes members to top-level,
// mutating the map) and before validateCheckBeds/validateIterateBed (which count root Plan checks).
func FlattenBundleVenues(uf *spec.UnifiedFile) error {
	if uf == nil || len(uf.Bundle) == 0 {
		return nil
	}
	for _, name := range SortedDeployKeys(uf.Bundle) {
		node := uf.Bundle[name]
		if err := flattenBundleOne(&node, name); err != nil {
			return err
		}
		uf.Bundle[name] = node
	}
	return nil
}

// flattenBundleOne flattens a single top-level bundle tree rooted at `root` (named rootName) in
// place.
func flattenBundleOne(root *spec.BundleNode, rootName string) error {
	// 1. Root's OWN direct steps run on the root's own venue (its container / host). A pure GROUP
	//    root (no cross-ref → empty Target) has no container, so a direct scored/run step there
	//    has nowhere to run.
	if root.Target == "" && len(root.Plan) > 0 {
		return fmt.Errorf("bundle %q is a group (no workload cross-ref) but carries %d direct plan step(s) — a group has no venue; place each step under a member/nested resource node", rootName, len(root.Plan))
	}
	for i := range root.Plan {
		root.Plan[i].Venue = rootName
	}
	// 2. Members (siblings) are addressed by their BARE name (FoldMembers promotes them to
	//    top-level; an agent-provisioned member resolves via the bare `charly-<name>` fallback).
	//    Nested children of the ROOT workload are addressed `rootName.child`.
	for _, mName := range SortedMemberKeys(root.Members) {
		hoistVenueSubtree(root, root.Members[mName], mName)
	}
	for _, cName := range deploykit.SortedNestedKeys(root.Children) {
		hoistVenueSubtree(root, root.Children[cName], rootName+"."+cName)
	}
	return nil
}

// hoistVenueSubtree stamps venuePath onto every step of `node`, appends those steps to root.Plan,
// clears node.Plan (so the steps run once, from the root plan), and recurses into node's nested
// children (dotted) and any sub-members (bare). venuePath is the dotted address the plugin scorer's
// chain resolver / ResolveDeployChain resolve.
func hoistVenueSubtree(root, node *spec.BundleNode, venuePath string) {
	if node == nil {
		return
	}
	for i := range node.Plan {
		s := node.Plan[i]
		s.Venue = venuePath
		root.Plan = append(root.Plan, s)
	}
	node.Plan = nil
	for _, cName := range deploykit.SortedNestedKeys(node.Children) {
		hoistVenueSubtree(root, node.Children[cName], venuePath+"."+cName)
	}
	// A member that is itself a group can carry sibling members — addressed bare (defensive; the
	// shipped beds nest via Children only).
	for _, mName := range SortedMemberKeys(node.Members) {
		hoistVenueSubtree(root, node.Members[mName], mName)
	}
}

// VenueIsAgentProvisioned reports whether the bare top-level venue name resolves to an
// agent-provisioned member/child anywhere in uf's bundle trees. Used by the host-target image
// preflight to SKIP venues whose image the AI builds in-run (they are not pullable).
// Agent-provisioned members are not folded to top-level, so the lookup walks each bed's in-tree
// members/children.
func VenueIsAgentProvisioned(uf *spec.UnifiedFile, venue string) bool {
	if uf == nil || venue == "" {
		return false
	}
	var walk func(n *spec.BundleNode) bool
	walk = func(n *spec.BundleNode) bool {
		if n == nil {
			return false
		}
		for k, child := range n.Children {
			if k == venue && child.AgentProvisioned {
				return true
			}
			if walk(child) {
				return true
			}
		}
		for k, member := range n.Members {
			if k == venue && member.AgentProvisioned {
				return true
			}
			if walk(member) {
				return true
			}
		}
		return false
	}
	for _, name := range SortedDeployKeys(uf.Bundle) {
		node := uf.Bundle[name]
		if walk(&node) {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// sibling `peer:` member fold (LOAD-half).
//
// A BundleNode's `peer:` map declares companion deployments brought up ALONGSIDE it on the shared
// `charly` network. FoldMembers registers each member as a top-level, addressable Bundle entry at
// load time (inheriting the owner's disposability), so a member is brought up/torn down by the SAME
// deploy verbs the deploy path already uses.
// -----------------------------------------------------------------------------

// FoldMembers copies every deploy node's `peer:` entries into the Bundle map as top-level
// addressable entries (MemberOf set, disposability inherited), so every deploy verb resolves a
// member by name through the same path as any deploy. Runs BEFORE validateDeploymentTree (so folded
// members get the same deploy validation); a check bed is itself a `disposable: true` bundle, so a
// bed's members fold the same way. A member name colliding with any existing deploy/member entry is
// a hard error.
func FoldMembers(uf *spec.UnifiedFile) error {
	if uf == nil || len(uf.Bundle) == 0 {
		return nil
	}
	// Collect first (we mutate the map below). Iterate a sorted owner list so a collision between
	// two owners' members is reported deterministically.
	type pendingMember struct {
		key        string
		node       spec.BundleNode
		owner      string
		disposable bool
	}
	var pending []pendingMember
	for _, owner := range SortedDeployKeys(uf.Bundle) {
		ownerNode := uf.Bundle[owner]
		for _, memberKey := range SortedMemberKeys(ownerNode.Members) {
			memberNode := ownerNode.Members[memberKey]
			if memberNode == nil {
				return fmt.Errorf("deploy %q peer %q is empty", owner, memberKey)
			}
			// An agent-provisioned member is deployed by the AI at run time (the iterate-benchmark
			// contract), NOT by the bed/deploy. Skip it: never a top-level addressable entry → no
			// auto bring-up, and no cross-bed name collision (the same venue name, e.g. `os`,
			// recurs across iterate beds). The scorer reaches its `charly-<name>` container via the
			// plugin scorer's bare-name fallback (candy/plugin-check's pluginResolveScoringChain).
			if memberNode.AgentProvisioned {
				continue
			}
			pending = append(pending, pendingMember{
				key:        memberKey,
				node:       *memberNode,
				owner:      owner,
				disposable: ownerNode.IsDisposable(),
			})
		}
	}
	for _, p := range pending {
		if _, clash := uf.Bundle[p.key]; clash {
			return fmt.Errorf(
				"peer name %q (declared under deploy %q) collides with an existing deploy/bed/peer entry — peer names must be globally unique; rename it",
				p.key, p.owner)
		}
		node := p.node
		node.MemberOf = p.owner
		// A companion inherits its owner's disposability so the owner's teardown/rebuild (e.g. a
		// kind:check bed's charly update) is authorized to destroy + rebuild it too.
		if p.disposable {
			disposable := true
			node.Disposable = &disposable
		}
		uf.Bundle[p.key] = node
	}
	return nil
}

// ValidateMembers enforces the member-specific invariants beyond the generic deploy validation
// (which already runs on the folded members): member keys carry no `.` (dots are reserved for nested
// dotted-path addressing) and reference a valid target kind. Pod-target members get the
// required-image: check via the generic validateDeploymentTree on the folded entry. Registry-free:
// the valid-target set is the CUE-derived spec.ResourceKinds (minus the targetless "group") — the
// SAME derivation the host's deployTargetWords uses (R3) — so a new deploy substrate is a valid
// member target without a core edit (boundary law clause D).
func ValidateMembers(uf *spec.UnifiedFile) error {
	if uf == nil {
		return nil
	}
	for _, owner := range SortedDeployKeys(uf.Bundle) {
		node := uf.Bundle[owner]
		for _, memberKey := range SortedMemberKeys(node.Members) {
			if err := spec.ValidateDeploymentName(memberKey, owner+" (peer)"); err != nil {
				return err
			}
			memberNode := node.Members[memberKey]
			if memberNode == nil {
				continue
			}
			// Kind-blind: a peer member's target is valid iff it is a recognized deploy substrate
			// (the empty target defaults to pod). NOT a compiled-in per-kind switch — because the
			// substrate kinds are plugin-served (C2-substrate), so a new external deploy substrate is
			// a valid member target without a core edit (the kernel/plugin boundary law).
			if !validMemberTarget(memberNode.Target) {
				return fmt.Errorf("deploy %q peer %q has unsupported target %q (not a recognized deploy substrate; \"\" defaults to pod)", owner, memberKey, memberNode.Target)
			}
		}
	}
	return nil
}

// validMemberTarget reports whether target is a valid peer-member deploy target: the empty target
// (which defaults to pod) or one of the canonical deploy substrates — spec.ResourceKinds minus the
// targetless "group" (the SAME set the host's deployTargetWords derives from spec.ResourceKinds, R3),
// so the consumer names no concrete kind word (the kernel/plugin boundary law).
func validMemberTarget(target string) bool {
	if target == "" {
		return true
	}
	if target == "group" {
		return false
	}
	for _, k := range spec.ResourceKinds {
		if k == target {
			return true
		}
	}
	return false
}
