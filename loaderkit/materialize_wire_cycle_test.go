package loaderkit

// materialize_wire_cycle_test.go — opencharly/opencharly#330: the materialized wire codec must
// TERMINATE a cyclic namespace graph.
//
// spec.UnifiedFile.Namespaces is JSON-serialized (it carries only `yaml:"-"`), while the loader's
// own contract says the graph IS cyclic: UnifiedFile.collectBeds guards against a mutual import
// (`main` imports `sub`, `sub` imports `main`), and a local-path `import:` inside the same git
// working tree mounts such a back-edge. A plain marshal of the nested *UnifiedFile tree is then
// rejected by encoding/json:
//
//	json: unsupported value: encountered a cycle via map[string]*spec.UnifiedFile
//
// which made `charly box validate` / `charly task <name>` fail on ANY such project. The wire must
// carry the namespace LEVELS flat (path-keyed), exactly as it already carries PluginKinds.
//
// Truncation semantics: a genuine back-edge (a namespace already on the current path) contributes
// nothing to any finite walk — UnifiedFile.collectBeds returns the instant it re-enters an
// ancestor — so the wire drops it rather than fabricating a node that would enumerate beds the
// source never exposes.

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestMarshalMaterialized_SelfCycleRoundTrips: a namespace that contains ITSELF must survive the
// marshal/unmarshal round-trip instead of failing the marshal. The self-alias is a back-edge, so it
// truncates; the root's own content is preserved.
func TestMarshalMaterialized_SelfCycleRoundTrips(t *testing.T) {
	self := &spec.UnifiedFile{RootDir: "/repo/self"}
	self.Namespaces = map[string]*spec.UnifiedFile{"self": self}

	data, err := MarshalMaterialized(self)
	if err != nil {
		t.Fatalf("MarshalMaterialized(self-cycle): %v", err)
	}
	var got spec.UnifiedFile
	if err := UnmarshalMaterialized(data, &got); err != nil {
		t.Fatalf("UnmarshalMaterialized: %v", err)
	}
	if got.RootDir != "/repo/self" {
		t.Fatalf("root RootDir = %q, want /repo/self", got.RootDir)
	}
	// The back-edge is restored as a POINTER to the reloaded root, not dropped: a box in a child
	// that bases on the alias (`base: <alias>.<box>`) must still resolve after the round-trip
	// (opencharly/charly#1295). The graph is cyclic by design, and every walk carries the
	// path-scoped guard, so restoring it fabricates nothing.
	if got.Namespaces["self"] != &got {
		t.Fatalf("self back-edge must restore as a pointer to the reloaded root, got %v", got.Namespaces["self"])
	}
}

// TestMarshalMaterialized_MutualCycleRoundTrips: the mutual import (main<->sub) that
// UnifiedFile.collectBeds names explicitly. `sub` is reachable (not an ancestor) and survives; the
// `sub.up` back-edge truncates.
func TestMarshalMaterialized_MutualCycleRoundTrips(t *testing.T) {
	main := &spec.UnifiedFile{RootDir: "/repo/main"}
	sub := &spec.UnifiedFile{RootDir: "/repo/sub"}
	main.Namespaces = map[string]*spec.UnifiedFile{"sub": sub}
	sub.Namespaces = map[string]*spec.UnifiedFile{"up": main} // the mutual-import back-edge

	data, err := MarshalMaterialized(main)
	if err != nil {
		t.Fatalf("MarshalMaterialized(mutual-cycle): %v", err)
	}
	var got spec.UnifiedFile
	if err := UnmarshalMaterialized(data, &got); err != nil {
		t.Fatalf("UnmarshalMaterialized: %v", err)
	}
	if got.RootDir != "/repo/main" {
		t.Fatalf("root RootDir = %q, want /repo/main", got.RootDir)
	}
	subGot := got.Namespaces["sub"]
	if subGot == nil || subGot.RootDir != "/repo/sub" {
		t.Fatalf("reachable namespace sub wrong: %+v", subGot)
	}
	if up := subGot.Namespaces["up"]; up != &got {
		t.Fatalf("mutual back-edge must restore as a pointer to the reloaded root, got %v", up)
	}
}

// TestMarshalMaterialized_NestedLevelsPreserved: a plain two-level tree (no cycle) still round-trips
// every level with its content — the flattening must not cost the ordinary nested case.
func TestMarshalMaterialized_NestedLevelsPreserved(t *testing.T) {
	root := &spec.UnifiedFile{RootDir: "/repo/root"}
	root.Namespaces = map[string]*spec.UnifiedFile{
		"a": {RootDir: "/repo/a", Namespaces: map[string]*spec.UnifiedFile{
			"b": {RootDir: "/repo/a/b"},
		}},
	}

	data, err := MarshalMaterialized(root)
	if err != nil {
		t.Fatalf("MarshalMaterialized(nested): %v", err)
	}
	var got spec.UnifiedFile
	if err := UnmarshalMaterialized(data, &got); err != nil {
		t.Fatalf("UnmarshalMaterialized: %v", err)
	}
	a := got.Namespaces["a"]
	if a == nil || a.RootDir != "/repo/a" {
		t.Fatalf("level a wrong: %+v", a)
	}
	b := a.Namespaces["b"]
	if b == nil || b.RootDir != "/repo/a/b" {
		t.Fatalf("level a.b wrong: %+v", b)
	}
}
