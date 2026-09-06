package loaderkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// vmBedThreaded recognizes the two substrate words the member-tree tests author (vm bed +
// pod member) with the DeployTraits the fold's target derivation consults, plus an external
// structural kind word and a plain kind word for the classification-unity cases.
var vmBedThreaded = spec.Threaded{
	Kinds:            map[string]bool{"pod": true, "vm": true, "distro": true},
	DeploySubstrates: map[string]bool{"pod": true, "vm": true, "exampledeploy": true},
	StructuralKinds:  map[string]bool{"pod": true, "vm": true, "extstruct": true},
	Primaries:        map[string]string{"http": "http"},
	DeployTraits: map[string]*spec.DeployTraits{
		"pod": {Venue: "container", ImageBacked: true},
		"vm":  {Venue: "ssh"},
	},
}

// TestParseFold_PreservesAuthoredDepth is the task-0 regression: a member authored INSIDE the
// kind body folds as an IN-SUBSTRATE member of that substrate (deploy-into), while a BARE
// deploy-level sibling of the kind key folds as a DEPLOY-LEVEL member (brought up alongside) —
// even under a WORKLOAD root (a vm bed), where the deleted root-kind branch used to collapse
// both positions into one meaning.
func TestParseFold_PreservesAuthoredDepth(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
bed:
  vm:
    from: golden
    sidecar:
      pod:
        image: img
  alpha:
    vm:
      from: vm-b
`), vmBedThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(pp.Nodes))
	}
	pn := pp.Nodes[0]
	// The parse: BOTH member children hang on the substrate node, in-body first.
	if len(pn.Children) != 2 || pn.Children[0].Name != "sidecar" || pn.Children[1].Name != "alpha" {
		t.Fatalf("children = %+v, want [sidecar, alpha]", pn.Children)
	}
	// The authored body keeps the in-body member key — the fold's position channel.
	body := bodyMap(t, pn.Body)
	if _, ok := body["sidecar"]; !ok {
		t.Fatalf("authored body dropped the in-body member key: %v", body)
	}
	dn, err := BuildFleetNode(pn, vmBedThreaded)
	if err != nil {
		t.Fatalf("BuildFleetNode: %v", err)
	}
	if dn.Target != "vm" || dn.From != "golden" {
		t.Fatalf("root node = %q/%q, want vm/golden", dn.Target, dn.From)
	}
	// The fold: ONE ordered member list, position stamped from the authored depth ALONE.
	if len(dn.Member) != 2 {
		t.Fatalf("member entries = %d, want 2", len(dn.Member))
	}
	inBody := dn.MemberByName("sidecar")
	if inBody == nil || inBody.Position != spec.PositionInSubstrate || !inBody.InSubstrate() {
		t.Fatalf("in-body member position = %+v, want in-substrate", inBody)
	}
	if inBody.Node == nil || inBody.Node.Target != "pod" || inBody.Node.Image != "img" {
		t.Fatalf("in-body member node = %+v, want pod/img", inBody.Node)
	}
	sibling := dn.MemberByName("alpha")
	if sibling == nil || sibling.Position != spec.PositionDeployLevel || !sibling.Alongside() {
		t.Fatalf("deploy-level sibling position = %+v, want deploy-level", sibling)
	}
	if sibling.Node == nil || sibling.Node.Target != "vm" || sibling.Node.From != "vm-b" {
		t.Fatalf("deploy-level member node = %+v, want vm/vm-b", sibling.Node)
	}
	// Authored depth survives RECURSIVELY: a member inside an in-body member nests too.
	_, pp2, err := ParseDoc(docFrom(t, `
bed:
  vm:
    from: golden
    sidecar:
      pod:
        image: img
        inner:
          local:
            from: hostbox
`), vmBedThreaded)
	if err != nil {
		t.Fatalf("ParseDoc (nested): %v", err)
	}
	dn2, err := BuildFleetNode(pp2.Nodes[0], vmBedThreaded)
	if err != nil {
		t.Fatalf("BuildFleetNode (nested): %v", err)
	}
	sc := dn2.MemberByName("sidecar")
	if sc == nil || sc.Node == nil || len(sc.Node.Member) != 1 {
		t.Fatalf("nested member tree = %+v, want sidecar carrying one member", dn2.Member)
	}
	if sc.Node.Member[0].Name != "inner" || sc.Node.Member[0].Position != spec.PositionInSubstrate {
		t.Fatalf("nested member position = %+v, want in-substrate inner", sc.Node.Member[0])
	}
}

// TestParse_MemberCarrierKeysStrippedFromBody pins the closedness contract: every body EMITTER
// omits the member-tree-owned keys, so the substrate's closed-schema gates stop seeing
// resource-member keys (Cutover C task 0) — while the authored body keeps them for the fold.
func TestParse_MemberCarrierKeysStrippedFromBody(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
bed:
  vm:
    from: golden
    sidecar:
      pod:
        image: img
`), vmBedThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	pn := pp.Nodes[0]
	if _, ok := bodyMap(t, pn.Body)["sidecar"]; !ok {
		t.Fatal("authored body lost the in-body member key")
	}
	clean, err := EntityBodyJSON(pn)
	if err != nil {
		t.Fatalf("EntityBodyJSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(clean, &m); err != nil {
		t.Fatalf("clean body json: %v", err)
	}
	if _, ok := m["sidecar"]; ok {
		t.Fatalf("EntityBodyJSON leaked the member-carrier key: %s", clean)
	}
	if m["from"] != "golden" {
		t.Fatalf("clean body lost the real data: %s", clean)
	}
	// The assembled body (the CUE-decode carrier) is stripped the same way.
	assembled, err := AssembleEntityBody(pn)
	if err != nil {
		t.Fatalf("AssembleEntityBody: %v", err)
	}
	root := spec.MappingRoot(assembled)
	if root == nil {
		t.Fatal("assembled body is not a mapping")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "sidecar" {
			t.Fatal("assembled body leaked the member-carrier key")
		}
	}
}

// TestMemberDisc_Unity pins the ONE classification: the parse-child arm (classifyKind asChild),
// the fold arm (IsResourceDisc), and memberDisc itself agree on every probe word — resource
// kinds, threaded structural kinds, threaded deploy substrates, and unknown words alike.
func TestMemberDisc_Unity(t *testing.T) {
	probe := []string{"pod", "vm", "group", "extstruct", "exampledeploy", "distro", "check", "not-a-word"}
	for _, w := range probe {
		want := memberDisc(w, vmBedThreaded)
		if got := classifyKind(w, true, vmBedThreaded); got != want {
			t.Errorf("classifyKind(%q, asChild) = %v, want %v (memberDisc unity)", w, got, want)
		}
		if got := IsResourceDisc(w, vmBedThreaded); got != want {
			t.Errorf("IsResourceDisc(%q) = %v, want %v (memberDisc unity)", w, got, want)
		}
	}
	if !memberDisc("extstruct", vmBedThreaded) {
		t.Error("memberDisc must consult structural kinds (the 8a fix)")
	}
	if !memberDisc("exampledeploy", vmBedThreaded) {
		t.Error("memberDisc must consult deploy substrates")
	}
	if memberDisc("distro", vmBedThreaded) {
		t.Error("a plain (non-structural) kind word is not a MEMBER discriminator")
	}
}

// TestParse_ExternalStructuralChildUnderExternalStructuralParent is the 8a regression: the
// former asChild arm (resourceKindSet only) could never classify an external structural child,
// so one under an external structural parent was a hard parse error. It parses now — and the
// child's own body parses as a member of it.
func TestParse_ExternalStructuralChildUnderExternalStructuralParent(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
parent:
  extstruct:
    cfg: x
  child:
    extstruct:
      cfg: y
`), vmBedThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	pn := pp.Nodes[0]
	if pn.Disc != "extstruct" || len(pn.Children) != 1 || pn.Children[0].Disc != "extstruct" {
		t.Fatalf("tree = %+v, want extstruct parent with one extstruct child", pn)
	}
	// A non-member kind still rejects members (parent-allow stays memberDisc-gated).
	if _, _, err := ParseDoc(docFrom(t, `
bad:
  distro:
    from: base
  mem:
    pod:
      image: i
`), vmBedThreaded); err == nil {
		t.Fatal("want a parent-allow error for a member under a non-member kind")
	}
}

// --- the no-dual-representation gates (Cutover C task 0, R5) ---

// TestDeployNodeHasNoDualMaps mirrors spec's own reflect gate: the dual Children/Members maps
// (and the dead Inside field) must not reappear on the deploy node.
func TestDeployNodeHasNoDualMaps(t *testing.T) {
	dead := []string{"Children", "Members", "Inside"}
	ft := reflect.TypeOf(spec.Deploy{})
	for _, f := range dead {
		if _, ok := ft.FieldByName(f); ok {
			t.Errorf("spec.Deploy grew back the dual-representation field %q", f)
		}
	}
}

// forbiddenDualPatterns — the removed map-era helpers and the dual-map CONSTRUCTION spellings
// (field ACCESS on ParsedNode.Children is legitimate parsed-tree surface, so the grep gates the
// constructors and the dead helpers, and the reflect gate above owns the type surface).
var forbiddenDualPatterns = []*regexp.Regexp{
	regexp.MustCompile(`SortedMemberKeys`),
	regexp.MustCompile(`SortedNestedKeys`),
	regexp.MustCompile(`\.HasChildren\(`),
	regexp.MustCompile(`Members:\s*map\[string\]\*(spec\.Deploy|spec\.FleetNode|FleetNode)`),
	regexp.MustCompile(`Children:\s*map\[string\]\*(spec\.Deploy|spec\.FleetNode|FleetNode)`),
}

// TestNoDualMapConstruction sweeps every non-test Go file in the module for the dual-map
// construction spellings and the removed map-era helpers (the B14 discipline: the claim is
// executed, not asserted).
func TestNoDualMapConstruction(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(thisFile)) // loaderkit/*.go → module root (go.mod)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".check" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		for _, re := range forbiddenDualPatterns {
			if m := re.Find(data); m != nil {
				t.Errorf("%s: dual-map residue %q matches %s", rel, m, re)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
}
