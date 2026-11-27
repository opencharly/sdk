package deploykit

// bed_session_test.go — the bed deploy persist→load round-trip (opencharly/charly#1299).
//
// A bed member declared `from: <vm>` persists through PersistBedDeployOverrides into the
// per-host overlay. Before the fix that call omitted SaveDeployStateInput.VmCrossRef, so the
// written entry was `vm: {disposable: true}` with no source; the next load dropped it as a
// degraded entry and the SDK save guard refused the following write:
//   "refusing to write … — it holds 2 deploy entr(y|ies) but only 1 load"
// which is exactly how `charly check run check-sidecar-pod` failed at [config].

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// loadNodeForm is the test's reader for the per-host node-form overlay: it decodes the shape
// MarshalDeployNode writes and the loader reads (`<name>: {<disc>: <Deploy body>}`) and DROPS a
// template-backed entry that names no same-kind source — the degraded-load rule that produced
// "holds 2 deploy entr(y|ies) but only 1 load" on check-sidecar-pod. A pod entry loads by its
// image.
func loadNodeForm(t *testing.T, path string) (*DeployConfig, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DeployConfig{Deploy: map[string]DeployNode{}}, nil
		}
		return nil, err
	}
	var doc map[string]map[string]spec.Deploy
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	dc := &DeployConfig{Deploy: map[string]DeployNode{}}
	for name, byDisc := range doc {
		for disc, node := range byDisc {
			node.Target = disc
			if disc != "pod" && node.Image == "" && node.From == "" {
				continue // source-less template-backed entry: the loader cannot unify it
			}
			dc.Deploy[name] = node
		}
	}
	return dc, nil
}

// TestPersistBedDeployOverrides_PersistsVmCrossRef proves a bed member declared `from: <vm>`
// keeps its source across the persist→load round-trip and is not reported as degraded.
//
// FAILS WITHOUT THE CHANGE: the persisted member is `vm: {disposable: true}` (From empty), so
// the decode above drops it, the follow-up save is refused by the degraded-load guard.
func TestPersistBedDeployOverrides_PersistsVmCrossRef(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)
	// A pre-existing, loadable entry: the member persists BESIDE an operator entry, so the
	// degraded-load guard has a second key to account for (the production "2 but only 1" shape).
	if err := os.WriteFile(dest, []byte("web:\n  pod:\n    image: web\n"), 0o600); err != nil {
		t.Fatalf("seed overlay: %v", err)
	}
	prev := DeployStateHost
	DeployStateHost = nil // the out-of-process plugin shape: the caller injects its reader
	t.Cleanup(func() { DeployStateHost = prev })

	reader := func() (*DeployConfig, error) { return loadNodeForm(t, dest) }
	marshalNode := func(_ string, node *DeployNode) (*yaml.Node, error) {
		return MarshalDeployNode(node, nil)
	}

	disp := true
	node := DeployNode{From: "eval-vm", Target: "vm", Disposable: &disp}
	PersistBedDeployOverrides(context.Background(), "check-sidecar-pod-ephvm", node, false, marshalNode, reader)

	// (1) The written entry names its source.
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read overlay: %v", err)
	}
	var doc map[string]map[string]spec.Deploy
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode overlay: %v", err)
	}
	member, ok := doc["check-sidecar-pod-ephvm"]["vm"]
	if !ok {
		t.Fatalf("persisted member entry missing; overlay:\n%s", data)
	}
	if member.From != "eval-vm" {
		t.Fatalf("persisted member From = %q, want eval-vm — the entry loses its source and the load drops it as degraded; overlay:\n%s", member.From, data)
	}

	// (2) Loaded back, BOTH entries load and the member still names its source.
	loaded, err := reader()
	if err != nil {
		t.Fatalf("reload overlay: %v", err)
	}
	if len(loaded.Deploy) != 2 {
		t.Fatalf("loaded %d deploy entries, want 2 — the member was dropped as a degraded load", len(loaded.Deploy))
	}
	if got := loaded.Deploy["check-sidecar-pod-ephvm"].From; got != "eval-vm" {
		t.Fatalf("loaded member From = %q, want eval-vm", got)
	}

	// (3) The next save (the bed's `charly config` step) is not refused by the degraded-load guard.
	if err := SaveDeployConfig(loaded, marshalNode, reader, context.Background()); err != nil {
		t.Fatalf("the next save was refused by the degraded-load guard: %v", err)
	}
}
