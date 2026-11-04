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

// TestFoldMembers_DiamondAliasFoldsSharedScopeOnce covers a REFERENCE mount: the loader can mount
// the SAME *UnifiedFile at two alias paths (namespace_walk.go's byID pointer identity), so the
// same member is visited twice through one scope. That second visit is idempotent — the same
// logical origin, so skip, not a collision.
func TestFoldMembers_DiamondAliasFoldsSharedScopeOnce(t *testing.T) {
	member := &spec.Deploy{Target: "vm", From: "eval-vm"}
	shared := &spec.UnifiedFile{
		RootDir: "/cache/repos/github.com/opencharly/distro-arch@v2026.274.1256.view.da6451d8a8f0b1c1",
		Deploy: map[string]spec.DeployNode{
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
	// ONE namespace UnifiedFile mounted at two alias paths — the shared REFERENCE-mount shape.
	root := &spec.UnifiedFile{Namespaces: map[string]*spec.UnifiedFile{
		"alias-a": shared,
		"alias-b": shared,
	}}

	if err := FoldMembers(root); err != nil {
		t.Fatalf("FoldMembers rejected a shared-scope diamond mount: %v", err)
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

// TestFoldMembers_DiamondAliasFoldsDistinctCopiesOnce is the REAL diamond shape — the one that
// broke `charly box validate` on sdk v0.2026308.351 (opencharly/sdk#484).
//
// The measured graph: a root project imports `arch` and `cachyos`, and cachyos imports the SAME
// `arch` repo@ref under its own `arch` namespace. Materialize produces two DISTINCT
// *spec.UnifiedFile scopes for arch and, inside them, distinct *spec.Deploy copies of the same
// logical member (node copies, not one shared pointer) — both resolved from the SAME repo cache
// dir, so both scopes carry the SAME RootDir. Keying idempotence on node POINTER identity (the
// pre-fix model, which only the synthetic shared-pointer test above ever produced) reports the
// second copy as a phantom collision.
//
// The fold's dedupe is therefore keyed on logical ORIGIN — (RootDir of the declaring project,
// declaring deploy, bare member key) — and this test builds the shape the loader actually emits.
func TestFoldMembers_DiamondAliasFoldsDistinctCopiesOnce(t *testing.T) {
	const archDir = "/cache/repos/github.com/opencharly/distro-arch@v2026.274.1256.view.da6451d8a8f0b1c1"
	// mkArchScope builds ONE independently-materialized copy of distro-arch's deploy; each call
	// allocates fresh Deploy/Member/Node values, exactly as the two mounts did. The `Node` pointer
	// is deliberately NOT shared between the copies.
	mkArchScope := func() *spec.UnifiedFile {
		return &spec.UnifiedFile{
			Repo:    "github.com/opencharly/distro-arch",
			RootDir: archDir,
			Deploy: map[string]spec.DeployNode{
				"check-arch-vm": {
					Target:     "pod",
					Disposable: foldTrue(),
					Member: []spec.Member{{
						Name:     "charly-cachyos-tailscale-test",
						Position: spec.PositionDeployLevel,
						Node:     &spec.Deploy{Target: "vm", From: "arch-host"},
					}},
				},
			},
		}
	}
	archA, archB := mkArchScope(), mkArchScope()
	if archA.Deploy["check-arch-vm"].Member[0].Node == archB.Deploy["check-arch-vm"].Member[0].Node {
		t.Fatal("fixture defect: the two mounts must carry DISTINCT node copies (that is the real shape)")
	}
	root := &spec.UnifiedFile{
		RootDir: "/tmp/project",
		Namespaces: map[string]*spec.UnifiedFile{
			"arch": archA,
			"cachyos": {
				RootDir: "/cache/repos/github.com/opencharly/distro-cachyos@v2026.281.1001.view.da6451d8a8f0b1c1",
				// cachyos's own deploy map (none) + its arch namespace, a SECOND materialization
				// of the same repo@ref.
				Namespaces: map[string]*spec.UnifiedFile{"arch": archB},
			},
		},
	}

	if err := FoldMembers(root); err != nil {
		t.Fatalf("FoldMembers rejected a real diamond (same repo@ref reached as `arch` and `cachyos.arch`, "+
			"distinct node copies): %v\n"+
			"  The logical origin is the same repo@ref, so the second fold is idempotent, not a collision.", err)
	}
	// The two mounts are ONE logical member, so it folds ONCE (into whichever scope the
	// nondeterministic namespace-order walk reaches first); the other keeps only its owner.
	folds := 0
	for name, scope := range map[string]*spec.UnifiedFile{"arch": archA, "cachyos.arch": archB} {
		folded, ok := scope.Deploy["charly-cachyos-tailscale-test"]
		if ok {
			folds++
			if folded.Target != "vm" || folded.MemberOf != "check-arch-vm" {
				t.Errorf("%s: folded diamond member = (target=%q, memberOf=%q), want (vm, check-arch-vm)",
					name, folded.Target, folded.MemberOf)
			}
			if len(scope.Deploy) != 2 {
				t.Errorf("%s: scope has %d deploy entries (%v), want exactly 2 (owner + one folded member)",
					name, len(scope.Deploy), SortedDeployKeys(scope.Deploy))
			}
			continue
		}
		if len(scope.Deploy) != 1 {
			t.Errorf("%s: non-folded scope has %d deploy entries (%v), want only the owner",
				name, len(scope.Deploy), SortedDeployKeys(scope.Deploy))
		}
	}
	if folds != 1 {
		t.Errorf("the same logical member folded %d times across the two mounts, want exactly 1", folds)
	}
}

// TestFoldMembers_SameBareNameFromDifferentOriginsIsAnError is the control that keeps the
// origin-based dedupe honest: a bare-name collision must NOT become a silent fold just because
// the two declarations happen to share an owner name. Two DIFFERENT repos (distinct RootDir)
// declaring the same deploying deploy and the same bare member name are genuinely different
// logical members — the bare CLI address is global, so this stays a hard load error.
func TestFoldMembers_SameBareNameFromDifferentOriginsIsAnError(t *testing.T) {
	mk := func(repo, rootDir string) *spec.UnifiedFile {
		return &spec.UnifiedFile{Repo: repo, RootDir: rootDir, Deploy: map[string]spec.DeployNode{
			"check-vm": {
				Target: "pod",
				Member: []spec.Member{{
					Name:     "shared-member",
					Position: spec.PositionDeployLevel,
					Node:     &spec.Deploy{Target: "vm", From: "eval-vm"},
				}},
			},
		}}
	}
	root := &spec.UnifiedFile{
		RootDir: "/tmp/project",
		Namespaces: map[string]*spec.UnifiedFile{
			"a": mk("github.com/opencharly/distro-a", "/cache/repos/github.com/opencharly/distro-a@v1"),
			"b": mk("github.com/opencharly/distro-b", "/cache/repos/github.com/opencharly/distro-b@v1"),
		},
	}

	if err := FoldMembers(root); err == nil {
		t.Fatal("FoldMembers folded the same bare member name from two DIFFERENT repos: " +
			"a different logical origin must remain a hard collision, never a silent fold")
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
