package loaderkit

// declared_fields_executor_test.go — the executor-path proof: the degraded declared-fields
// Threaded (∅ maps — the pre-gate registry state) delivered through the EXECUTOR SEAM
// (spec.LoaderExecutor, the contract LoadUnifiedViaExecutor builds its LoadSeams from) parses
// a converted tree on disk IDENTICALLY to the populated host-side parse — the full
// loaderkit.LoadUnified orchestration, not just the parse seam. The mock's legs mirror the
// executorLoaderExecutor wiring: the walk runs the kind-blind loaderkit.Walk with the Threaded
// callback the host threads (here: the degraded snapshot), the DATA seams call
// exec.LoaderThreaded() FRESH, exactly as load_via_executor.go wires them.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// degradedExecutorMock is the spec.LoaderExecutor witness whose registry snapshot carries the
// recognition DATA but NOT the declared-fields maps — the pre-gate state the executor legs
// deliver when the host process's schema gate has not run (the live-repro state).
type degradedExecutorMock struct{ threaded func() spec.Threaded }

func (e *degradedExecutorMock) LoaderThreaded() spec.Threaded { return e.threaded() }
func (e *degradedExecutorMock) RunBootstrapPhase(data []byte) ([]byte, error) {
	return data, nil
}
func (e *degradedExecutorMock) WalkProject(dir string, rootData []byte) (spec.LoadedProject, error) {
	// The host "loader-walk" leg's shape: the kind-blind Walk over the host-threaded seams.
	return Walk(dir, rootData, "", spec.WalkSeams{
		Parser:   DocParser{},
		Threaded: e.threaded,
		Boundary: func(string, []byte) error { return nil },
		GateDoc:  func(string, []byte) error { return nil },
	})
}
func (e *degradedExecutorMock) MaterializeLoadedProject(lp *spec.LoadedProject, merged *spec.UnifiedFile, _ map[int64]*spec.UnifiedFile) error {
	// The minimal registry-free materialize: each top-level parsed node builds into the Fleet
	// via the SAME pure BuildFleetNode decode production uses (the CUE body decode consults
	// the embedded contract, never the registry), and the merged Version carries the tree
	// past the schema gate.
	merged.Version = kit.LatestSchemaVersion().String()
	if merged.Fleet == nil {
		merged.Fleet = map[string]spec.FleetNode{}
	}
	for i := range lp.Docs {
		for _, pn := range lp.Docs[i].Project.Nodes {
			dn, err := BuildFleetNode(pn, e.threaded())
			if err != nil {
				return err
			}
			merged.Fleet[pn.Name] = *dn
		}
	}
	return nil
}
func (*degradedExecutorMock) ValidateAndroidDevices(*spec.UnifiedFile) error { return nil }
func (*degradedExecutorMock) ValidatePreemptible(*spec.UnifiedFile) error    { return nil }

// TestLoadUnifiedViaExecutorSeam_DeclaredFieldsFloor drives the FULL LoadUnified orchestration
// through the executor seam with the degraded Threaded: the converted tree (the live-repro
// check-agent-live shape) must load clean — the declared `iterate:` field stays DATA through
// the walk parse, the #223 KEY rule and the #225 substrate exclusion live PLUGIN-SIDE, and the
// live-repro `node "sandbox": expected a mapping value` error cannot re-arm. The same tree
// through a POPULATED executor snapshot loads to the same Fleet tree.
func TestLoadUnifiedViaExecutorSeam_DeclaredFieldsFloor(t *testing.T) {
	dir := t.TempDir()
	root := "version: \"" + kit.LatestSchemaVersion().String() + "\"\n" + convertedTree
	if err := os.WriteFile(filepath.Join(dir, spec.UnifiedFileName), []byte(root), 0o644); err != nil {
		t.Fatalf("write tree: %v", err)
	}

	uf, ok, err := LoadUnified(dir, LoadSeamsFromExecutor(&degradedExecutorMock{threaded: degradedExecutorThreaded}))
	if err != nil {
		t.Fatalf("LoadUnified (degraded executor Threaded): %v", err)
	}
	if !ok || uf == nil {
		t.Fatalf("expected the project to load")
	}
	if len(uf.Fleet) != 1 {
		t.Fatalf("expected 1 fleet node, got %d", len(uf.Fleet))
	}
	node, ok := uf.Fleet["check-agent-live"]
	if !ok {
		t.Fatalf("the converted tree's pod node must materialize: %v", uf.Fleet)
	}
	// The declared iterate: field must survive as DATA — decoded into the typed Iterate
	// projection with its sandbox scalar intact, never re-scanned into the member tree,
	// never dropped.
	if node.Iterate == nil || node.Iterate.Sandbox != "check-agent-pod" {
		t.Fatalf("the declared iterate data did not survive the executor-seam load: %+v", node.Iterate)
	}
	if len(node.Iterate.Agent) != 1 || node.Iterate.Agent[0] != "check-agent-live-claude" {
		t.Fatalf("the iterate agent catalog did not survive: %+v", node.Iterate.Agent)
	}

	// Parity: the SAME tree through the POPULATED executor snapshot produces the same fleet.
	uf2, ok, err := LoadUnified(dir, LoadSeamsFromExecutor(&degradedExecutorMock{threaded: hostPopulatedThreaded}))
	if err != nil {
		t.Fatalf("LoadUnified (populated executor Threaded): %v", err)
	}
	if !ok || uf2 == nil || len(uf2.Fleet) != 1 {
		t.Fatalf("populated control load failed: %v", uf2)
	}
	if len(node.Member) != len(uf2.Fleet["check-agent-live"].Member) {
		t.Fatalf("member trees diverge between the degraded and populated executor snapshots: %d vs %d", len(node.Member), len(uf2.Fleet["check-agent-live"].Member))
	}
}
