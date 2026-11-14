package loaderkit

import "testing"

// TestCueKindDefsLocalNamesTheContractEnvelope pins the entry opencharly/spec#304 unit 4
// changed. The authored `#Local` body lives in the plugin that serves the kind, so the
// contract module no longer declares it; naming it here panics the loader at first use
// (measured: `loaderkit: CUE kind "local": definition #Local not found`). The entry must name
// the envelope the contract DOES keep, and that def must resolve in the embedded schema.
//
// A grep for the authored def name across this package returns this comment block, the note
// beside the table in cue_schema.go, and this test's own failure message — three comments and
// one string literal, and no compiled reference: the def is named only to say it is gone.
func TestCueKindDefsLocalNamesTheContractEnvelope(t *testing.T) {
	if got := cueKindDefs["local"]; got != "#ResolvedLocal" {
		t.Fatalf("cueKindDefs[local] = %q, want %q — the authored #Local body is not in this contract (spec#304 unit 4)", got, "#ResolvedLocal")
	}
	d, ok := cueKindDef("local")
	if !ok {
		t.Fatal("cueKindDef(local) reported the kind unknown")
	}
	if err := d.Err(); err != nil {
		t.Fatalf("the contract declares no #ResolvedLocal: %v", err)
	}
}
