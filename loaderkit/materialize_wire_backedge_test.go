package loaderkit

// materialize_wire_backedge_test.go — opencharly/charly#1295: a box reached through an import must
// still resolve a `base:` that names the AUTHORING project's own alias after the graph crosses the
// executor wire.
//
// The case is the real one: distro-cachyos imports pod-whisper, and pod-whisper imports
// distro-cachyos under the alias `cachyos`; pod-whisper's `whisper` box bases on `cachyos.cachyos`.
// Inside distro-cachyos that alias is a BACK-EDGE (the ancestor the loader already mounted), so the
// wire used to drop it — and `build:`/`distro:` came back missing, surfacing two stages later as
// `build: field required` and `DistroDef == nil`.

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestRoundTripResolvesABaseThroughTheImportBackEdge(t *testing.T) {
	root := &spec.UnifiedFile{RootDir: "/repo/distro-cachyos", Box: spec.BoxMap{
		"cachyos": []byte(`{"base":"docker.io/cachyos/cachyos-v3:latest","build":["pac"]}`),
	}}
	child := &spec.UnifiedFile{RootDir: "/repo/pod-whisper", Box: spec.BoxMap{
		"whisper": []byte(`{"base":"cachyos.cachyos","distro":{"pac":{"install":"pacman -S"}}}`),
	}}
	root.Namespaces = map[string]*spec.UnifiedFile{"pod-whisper": child}
	child.Namespaces = map[string]*spec.UnifiedFile{"cachyos": root} // the mutual import

	data, err := MarshalMaterialized(root)
	if err != nil {
		t.Fatalf("MarshalMaterialized: %v", err)
	}
	var got spec.UnifiedFile
	if err := UnmarshalMaterialized(data, &got); err != nil {
		t.Fatalf("UnmarshalMaterialized: %v", err)
	}

	cfg := got.ProjectConfig()
	sub, ok := cfg.Namespaces["pod-whisper"]
	if !ok {
		t.Fatal("the imported namespace must survive the round-trip")
	}
	if sub.Namespaces["cachyos"] == nil {
		t.Fatal("the back-edge alias `cachyos` must be restored: it is what the child's base resolves through")
	}
	// The mechanism the renderer uses: resolve the qualified base, then inherit build: across it.
	if _, _, internal := sub.ResolveBoxRef("cachyos.cachyos"); !internal {
		t.Fatal("`cachyos.cachyos` must resolve as an INTERNAL box through the restored back-edge")
	}
	build := sub.WalkBaseChainBuild("cachyos.cachyos")
	if len(build) != 1 || build[0] != "pac" {
		t.Fatalf("build: inherited through the back-edge = %v, want [pac] (the empty answer is the filed defect)", build)
	}
}
