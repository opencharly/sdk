package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// foldTrue is the tiny *bool helper the fixtures need for spec.Deploy.Disposable.
func foldTrue() *bool { b := true; return &b }

// TestFoldMembers_RecursesNamespaces is the failing-before regression for
// opencharly/charly#1083.
//
// THE DEFECT: FoldMembers folded only the ROOT uf.Deploy map's deploy-level members. A deploy
// imported under a namespace (the umbrella project imports charly/ as `charly`) keeps its
// deploy-level members hanging inside the owner node's Member list and never gains the
// top-level addressable entry the Cutover C addressing law promises (deploy_load.go's own
// header: "DEPLOY-LEVEL members ... are addressed by their BARE name"). The re-entrant
// `charly deploy add <member>` then resolves NO node, the substrate word is never collected,
// its provider is never connected, and the dispatch falls back to pod — refusing a substrate the
// project does compose, after the member's VM already exists.
//
// The fix is the one walk the sibling whole-tree stamps already use: recurse uf.Namespaces with
// the path-scoped NamespaceAncestors guard (StampDeployDescents, collectDeploys' sibling). This
// test drives that at the smallest seam — a synthetic two-scope tree, no project load.
func TestFoldMembers_RecursesNamespaces(t *testing.T) {
	ns := &spec.UnifiedFile{
		Deploy: map[string]spec.DeployNode{
			"check-sidecar-pod": {
				Target:     "pod",
				Disposable: foldTrue(),
				Member: []spec.Member{{
					Name:     "check-sidecar-pod-ephvm",
					Position: spec.PositionDeployLevel,
					Node:     &spec.Deploy{Target: "vm", From: "eval-vm"},
				}},
			},
		},
	}
	root := &spec.UnifiedFile{
		Namespaces: map[string]*spec.UnifiedFile{"charly": ns},
	}

	if err := FoldMembers(root); err != nil {
		t.Fatalf("FoldMembers: %v", err)
	}

	folded, ok := ns.Deploy["check-sidecar-pod-ephvm"]
	if !ok {
		t.Fatalf("FoldMembers did not fold the deploy-level member of a NAMESPACE-imported deploy; "+
			"namespace deploy keys = %v\n"+
			"  This is opencharly/charly#1083: the member is addressable by its bare name by law, "+
			"but no top-level entry exists in the scope that owns it, so `charly deploy add %q` "+
			"resolves no node and defaults its substrate to pod.",
			SortedDeployKeys(ns.Deploy), "check-sidecar-pod-ephvm")
	}
	if folded.Target != "vm" {
		t.Errorf("folded member Target = %q, want vm", folded.Target)
	}
	if folded.From != "eval-vm" {
		t.Errorf("folded member From = %q, want the authored eval-vm (root qualification is the projection's job)", folded.From)
	}
	if folded.MemberOf != "check-sidecar-pod" {
		t.Errorf("folded member MemberOf = %q, want check-sidecar-pod", folded.MemberOf)
	}
	if !folded.IsDisposable() {
		t.Error("folded member did not inherit its owner's disposability")
	}
}

// TestFoldMembers_NamespaceMemberNameCollisionIsAnError pins the control that makes the bare-name
// law safe: a deploy-level member is addressed by its BARE name in the merged-root projection, so
// the name is GLOBAL — two scopes declaring the same member name must be a hard load error, never a
// silent last-wins map overwrite.
func TestFoldMembers_NamespaceMemberNameCollisionIsAnError(t *testing.T) {
	mk := func(owner string) *spec.UnifiedFile {
		return &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{
			owner: {
				Target: "pod",
				Member: []spec.Member{{
					Name:     "shared-member",
					Position: spec.PositionDeployLevel,
					Node:     &spec.Deploy{Target: "vm", From: "eval-vm"},
				}},
			},
		}}
	}
	root := &spec.UnifiedFile{Namespaces: map[string]*spec.UnifiedFile{
		"a": mk("bed-a"),
		"b": mk("bed-b"),
	}}

	if err := FoldMembers(root); err == nil {
		t.Fatal("FoldMembers accepted the same bare deploy-level member name in two scopes; " +
			"the bare address is global, so this must be a hard collision error")
	}
}

// TestFoldMembers_DiamondAliasFoldsSameMemberNodeOnce covers the same-node-pointer arm of the
// `folded` guard in foldMembers:
//
//	if prev, seen := folded[memberKey]; seen {
//	    if prev == memberNode { continue }
//	    ...
//	}
//
// A diamond/multi-alias mount reaches the SAME *UnifiedFile by more than one path BY DESIGN
// (namespace_walk.go: one repo at `arch` + `cachyos.arch`, or `c` through both `a` and `b`), so
// the same member node POINTER is collected twice under the same bare name. The second visit must
// be idempotent — skip, not a collision error — and the member must end up folded exactly once.
// Without the `prev == memberNode` arm the second alias reports a phantom collision.
func TestFoldMembers_DiamondAliasFoldsSameMemberNodeOnce(t *testing.T) {
	member := &spec.Deploy{Target: "vm", From: "eval-vm"}
	shared := &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{
		"bed": {
			Target:     "pod",
			Disposable: foldTrue(),
			Member: []spec.Member{{
				Name:     "diamond-member",
				Position: spec.PositionDeployLevel,
				Node:     member,
			}},
		},
	}}
	// ONE namespace UnifiedFile mounted at two alias paths — the diamond shape.
	root := &spec.UnifiedFile{Namespaces: map[string]*spec.UnifiedFile{
		"alias-a": shared,
		"alias-b": shared,
	}}

	if err := FoldMembers(root); err != nil {
		t.Fatalf("FoldMembers rejected a diamond/multi-alias mount: %v\n"+
			"  The same member node pointer reached by two alias paths is idempotent, not a collision.", err)
	}
	folded, ok := shared.Deploy["diamond-member"]
	if !ok {
		t.Fatalf("diamond member was not folded (scope deploy keys: %v)", SortedDeployKeys(shared.Deploy))
	}
	if folded.Target != "vm" || folded.MemberOf != "bed" {
		t.Errorf("folded diamond member = (target=%q, memberOf=%q), want (vm, bed)", folded.Target, folded.MemberOf)
	}
	// Exactly one fold: the owner plus the single folded entry, no duplicate and no error.
	if len(shared.Deploy) != 2 {
		t.Errorf("shared scope has %d deploy entries (%v), want exactly 2 (owner + one folded member)",
			len(shared.Deploy), SortedDeployKeys(shared.Deploy))
	}
}

// TestFoldMembers_MemberShadowingRootDeployIsAnError covers the rootTopLevel arm of foldMembers:
// the folded member is addressed by its BARE name in the merged-root projection, which is the SAME
// key space a ROOT top-level deploy occupies. A namespace member whose bare name equals a root
// deploy would silently overwrite that deploy in the projection, so the fold must reject it before
// any map write.
func TestFoldMembers_MemberShadowingRootDeployIsAnError(t *testing.T) {
	root := &spec.UnifiedFile{
		Deploy: map[string]spec.DeployNode{
			"shadowed": {Target: "pod", Image: "root-app"},
		},
		Namespaces: map[string]*spec.UnifiedFile{
			"charly": {Deploy: map[string]spec.DeployNode{
				"bed": {
					Target: "pod",
					Member: []spec.Member{{
						Name:     "shadowed",
						Position: spec.PositionDeployLevel,
						Node:     &spec.Deploy{Target: "vm", From: "eval-vm"},
					}},
				},
			}},
		},
	}

	err := FoldMembers(root)
	if err == nil {
		t.Fatal("FoldMembers accepted a deploy-level member whose bare name shadows a ROOT top-level deploy; " +
			"the merged-root projection would silently overwrite the root entry, so this must be a hard error")
	}
	if !strings.Contains(err.Error(), "root top-level deploy") {
		t.Errorf("error does not name the root-shadow collision: %v", err)
	}
}
