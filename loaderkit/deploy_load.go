package loaderkit

// deploy_load.go — the PURE, registry-free LOAD-half of the deploy passes (K1-LOADER
// RELOCATION, moved from charly/node_deploy_venue.go + charly/deploy_members.go). These are
// the LoadSeams.FlattenVenuesByPosition / FoldMembers steps plus the two shared sort helpers they
// (and the DEPLOY-half that STAYS core — bringUpMembers/tearDownMembers) rely on. Every function
// here operates ONLY on the already-materialized *spec.UnifiedFile / spec.DeployNode maps with zero
// provider-registry or host coupling (boundary law clause M: a kind-blind mechanism consuming an
// envelope), so it runs identically host-side OR plugin-side — the property that lets
// loaderkit.LoadUnified wire these seams directly without a reverse-channel hop. Behaviour is
// byte-identical to the former charly copies.
//
// The DEPLOY-half — bringUpMembers / tearDownMembers / isPodMember / isVmMember / withMemberTag —
// STAYS host-resident (charly/deploy_members.go) per the lead's U1 SPLIT ruling: it shells out via
// proc.RunCharlySubcommand + reads the live provider registry (nodeTraits), so it is NOT
// registry-free and does NOT belong here.

import (
	"fmt"

	"github.com/opencharly/spec/spec"
)

// SortedDeployKeys / VenueIsAgentProvisioned are DEFINED in the dedicated spec module
// (spec/spec/deploy_keys.go, #55 2b Class A) — pure deploy-map helpers with no kind-specific
// logic. The forwarder keeps loaderkit's own callers (FoldMembers / FlattenVenuesByPosition here) +
// charly's DEPLOY-half owner-walk terse (R3, one shared abstraction). The former member-key
// sort forwarder DIED with the dual maps: the ONE ordered Member list is deterministic in
// authored order, so there is nothing left to sort (Cutover C task 0).
var (
	SortedDeployKeys        = spec.SortedDeployKeys
	VenueIsAgentProvisioned = spec.VenueIsAgentProvisioned
)

// -----------------------------------------------------------------------------
// venue-from-position for deploy plan steps.
//
// In the unified node-form model a step's EXECUTION VENUE comes ENTIRELY from its POSITION in the
// deploy tree — there is no authored `on:`/`pod:` override (both retired). FlattenVenuesByPosition runs
// at load time, AFTER the tree is built and BEFORE FoldMembers + validation, and:
//
//   1. stamps every step's `venue` (Op.Venue) from its tree position:
//        - a step directly under a WORKLOAD root R          → "R"
//        - a step under a sibling MEMBER M                  → "M"            (bare)
//        - a step under a NESTED child C of parent path P   → "P.C"         (dotted)
//   2. HOISTS every member/child step into the ROOT deploy's flat Plan (and clears the
//      member/child Plan), because both runner entry points read the root node.Plan.
//
// A direct step under a pure GROUP root (no workload container) is a hard error — a group has no
// venue of its own; place the step under a member.
// -----------------------------------------------------------------------------

// FlattenVenuesByPosition stamps venue + hoists plan steps for every top-level deploy in uf.
// Idempotent on an already-flattened tree (members/children have empty Plan after the first pass,
// so re-running hoists nothing). Must run before FoldMembers (which promotes members to top-level,
// mutating the map) and before validateCheckBeds/validateIterateBed (which count root Plan checks).
func FlattenVenuesByPosition(uf *spec.UnifiedFile) error {
	if uf == nil || len(uf.Deploy) == 0 {
		return nil
	}
	for _, name := range SortedDeployKeys(uf.Deploy) {
		node := uf.Deploy[name]
		if err := flattenDeployOne(&node, name); err != nil {
			return err
		}
		uf.Deploy[name] = node
	}
	return nil
}

// flattenDeployOne flattens a single top-level deploy tree rooted at `root` (named rootName) in
// place.
func flattenDeployOne(root *spec.DeployNode, rootName string) error {
	// 1. Root's OWN direct steps run on the root's own venue (its container / host). A pure GROUP
	//    root (no cross-ref → empty Target) has no container, so a direct scored/run step there
	//    has nowhere to run.
	if root.Target == "" && len(root.Plan) > 0 {
		return fmt.Errorf("deploy %q is a group (no workload cross-ref) but carries %d direct plan step(s) — a group has no venue; place each step under a member/nested resource node", rootName, len(root.Plan))
	}
	for i := range root.Plan {
		root.Plan[i].Venue = rootName
	}
	// 2. DEPLOY-LEVEL members (siblings of the kind key) are addressed by their BARE name
	//    (FoldMembers promotes them to top-level; an agent-provisioned member resolves via
	//    the bare `charly-<name>` fallback). IN-SUBSTRATE members (entity keys inside the
	//    kind body) are addressed `rootName.child` — the venue comes from the member's
	//    stamped POSITION (the fold's authored-depth stamp), never from the node's kind
	//    (the dead root-kind branch).
	for _, m := range root.DeployLevelMembers() {
		hoistVenueSubtree(root, m.Node, m.Name)
	}
	for _, m := range root.InSubstrateMembers() {
		hoistVenueSubtree(root, m.Node, rootName+"."+m.Name)
	}
	return nil
}

// hoistVenueSubtree stamps venuePath onto every step of `node`, appends those steps to root.Plan,
// clears node.Plan (so the steps run once, from the root plan), and recurses into node's nested
// children (dotted) and any sub-members (bare). venuePath is the dotted address the plugin scorer's
// chain resolver / ResolveDeployChain resolve.
func hoistVenueSubtree(root, node *spec.DeployNode, venuePath string) {
	if node == nil {
		return
	}
	for i := range node.Plan {
		s := node.Plan[i]
		s.Venue = venuePath
		root.Plan = append(root.Plan, s)
	}
	node.Plan = nil
	for _, m := range node.InSubstrateMembers() {
		hoistVenueSubtree(root, m.Node, venuePath+"."+m.Name)
	}
	// A member that is itself a group can carry deploy-level members — addressed bare.
	for _, m := range node.DeployLevelMembers() {
		hoistVenueSubtree(root, m.Node, m.Name)
	}
}

// -----------------------------------------------------------------------------
// sibling `peer:` member fold (LOAD-half).
//
// A DeployNode's `peer:` map declares companion deployments brought up ALONGSIDE it on the shared
// `charly` network. FoldMembers registers each member as a top-level, addressable Deploy entry at
// load time (inheriting the owner's disposability), so a member is brought up/torn down by the SAME
// deploy verbs the deploy path already uses.
// -----------------------------------------------------------------------------

// FoldMembers copies every deploy node's deploy-level members into the Deploy map as top-level
// addressable entries (MemberOf set, disposability inherited), so every deploy verb resolves a
// member by name through the same path as any deploy. Runs BEFORE validateDeploymentTree (so folded
// members get the same deploy validation); a check bed is itself a `disposable: true` deploy, so a
// bed's members fold the same way. A member name colliding with any existing deploy/member entry is
// a hard error.
//
// It recurses into EVERY imported NAMESPACE (nested `nsA.nsB`), not just the root map: an imported
// deploy's deploy-level member is the SAME addressable entry as a local one (the Cutover C bare-name
// law), and without the recursion the merged-root tree never contains it — the re-entrant
// `charly deploy add <member>` resolves no node, collects no substrate word, connects no provider,
// and falls back to pod (opencharly/charly#1083). The sibling whole-tree stamps
// (StampDeployDescents, Deploys' own namespace walk) already recurse; this is the same walk with the
// same path-scoped NamespaceAncestors guard.
//
// The folded entry is addressed by its BARE name in the merged-root projection (UnifiedFile.Deploys
// skips the namespace prefix for a MemberOf-stamped entry), so the name is GLOBAL: two
// DIFFERENT logical members declaring the same bare name — or a member shadowing a ROOT
// top-level deploy — is a hard error, never a silent map overwrite.
//
// A diamond/multi-alias graph reaches the SAME logical member more than once BY DESIGN
// (namespace_walk.go), and materializing it through two paths yields DISTINCT *spec.Deploy
// copies — not one shared pointer — so the idempotence test must key on the member's LOGICAL
// ORIGIN, never on node pointer identity (opencharly/sdk#484). memberOrigin is that identity:
// the resolved project directory that declares the member (uf.RootDir, the loader's existing
// CanonicalRef mount identity: repo@ref/subpath for a remote import, the absolute directory for
// a local one), the declaring deploy's name within it, and the bare member key. Every mount of
// one repo@ref resolves to the SAME RootDir, however many aliases reach it, so a repeated fold
// is idempotent; a different repo, ref or subpath resolves to a different RootDir and remains a
// hard collision.
func FoldMembers(uf *spec.UnifiedFile) error {
	if uf == nil {
		return nil
	}
	// Root-scope top-level deploy names share the BARE key space a folded member occupies in the
	// merged-root projection, so a member anywhere may not shadow one.
	rootTopLevel := make(map[string]bool, len(uf.Deploy))
	for name := range uf.Deploy {
		rootTopLevel[name] = true
	}
	return foldMembers(uf, NamespaceAncestors{}, rootTopLevel, map[string]memberOrigin{}, map[string]string{})
}

// memberOrigin is a deploy-level member's LOGICAL origin — the unit the bare-name law makes
// unique. rootDir is the loader-resolved project directory that DECLARES the member; it is the
// existing mount identity CanonicalRef produces (the same string for every mount of one
// repo@ref), so it is reused rather than re-deriving an origin notion. scope is the fallback for
// a synthetic in-memory tree (RootDir empty — never a loader result), where the *UnifiedFile
// pointer stands in for the unresolved mount so distinct scopes still collide. owner is the
// declaring deploy's name inside that project. The member's bare key is the fold map's key.
type memberOrigin struct {
	rootDir string
	scope   *spec.UnifiedFile
	owner   string
}

// memberOriginOf returns the logical origin of a member declared by owner in uf. It is
// comparable, so the fold's dedupe is one struct comparison.
func memberOriginOf(uf *spec.UnifiedFile, owner string) memberOrigin {
	o := memberOrigin{owner: owner}
	if uf.RootDir != "" {
		o.rootDir = uf.RootDir
	} else {
		o.scope = uf
	}
	return o
}

func foldMembers(uf *spec.UnifiedFile, ancestors NamespaceAncestors, rootTopLevel map[string]bool, folded map[string]memberOrigin, owners map[string]string) error {
	leave, ok := ancestors.Enter(uf)
	if !ok {
		return nil
	}
	defer leave()
	// Collect first (we mutate the map below). Iterate a sorted owner list so a collision between
	// two owners' members is reported deterministically.
	type pendingMember struct {
		key        string
		node       spec.DeployNode
		owner      string
		disposable bool
	}
	var pending []pendingMember
	for _, owner := range SortedDeployKeys(uf.Deploy) {
		ownerNode := uf.Deploy[owner]
		// The DEPLOY-LEVEL members fold (registered top-level, MemberOf stamped); an
		// in-substrate member stays inside its owner's venue tree and is addressed by
		// its dotted path.
		for _, m := range ownerNode.DeployLevelMembers() {
			memberKey, memberNode := m.Name, m.Node
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
			// The bare address is GLOBAL. A diamond/multi-alias mount reaches the SAME logical
			// member by a second path — as a DISTINCT node copy, not a shared pointer — so the
			// idempotence test keys on the member's logical ORIGIN, never on node identity. A
			// member from a DIFFERENT origin claiming the same bare name is a genuine collision.
			origin := memberOriginOf(uf, owner)
			if prev, seen := folded[memberKey]; seen {
				if prev == origin {
					continue
				}
				return fmt.Errorf(
					"deploy-level member %q (declared under deploy %q) collides with the member of deploy %q — deploy-level member names are globally unique (the bare name is the CLI address); rename one",
					memberKey, owner, owners[memberKey])
			}
			if rootTopLevel[memberKey] {
				return fmt.Errorf(
					"deploy-level member %q (declared under deploy %q) collides with a root top-level deploy of the same bare name — deploy-level member names are globally unique (the bare name is the CLI address); rename one",
					memberKey, owner)
			}
			folded[memberKey] = origin
			owners[memberKey] = owner
			pending = append(pending, pendingMember{
				key:        memberKey,
				node:       *memberNode,
				owner:      owner,
				disposable: ownerNode.IsDisposable(),
			})
		}
	}
	for _, p := range pending {
		if _, clash := uf.Deploy[p.key]; clash {
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
		uf.Deploy[p.key] = node
	}
	for _, sub := range uf.Namespaces {
		if err := foldMembers(sub, ancestors, rootTopLevel, folded, owners); err != nil {
			return err
		}
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
	for _, owner := range SortedDeployKeys(uf.Deploy) {
		node := uf.Deploy[owner]
		for i := range node.Member {
			memberKey, memberNode := node.Member[i].Name, node.Member[i].Node
			if err := spec.ValidateDeploymentName(memberKey, owner+" (peer)"); err != nil {
				return err
			}
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
