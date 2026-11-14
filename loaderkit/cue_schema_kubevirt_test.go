package loaderkit

import "testing"

// TestCueKindDefsKubevirtNamesTheContractEnvelope pins the entry opencharly/spec#304 unit 6
// changed. The authored `#KubeVirt` body lives in the plugin that serves the kind now
// (opencharly/plugin-substrate#37), so the contract will stop declaring it; this table reads the
// BASE schema only, so naming the authored def here would panic at first use once that leg lands
// (the failure `pod`, `local` and `kubernetes` each taught). The entry must name the envelope the
// contract DOES keep — note the capital V: `#ResolvedKubeVirt` differs from the kind word by one
// letter, and a case-folded grep is what finds it.
func TestCueKindDefsKubevirtNamesTheContractEnvelope(t *testing.T) {
	if got := cueKindDefs["kubevirt"]; got != "#ResolvedKubeVirt" {
		t.Fatalf("cueKindDefs[kubevirt] = %q, want %q — the authored #KubeVirt body is moving out of this contract (spec#304 unit 6)", got, "#ResolvedKubeVirt")
	}
	d, ok := cueKindDef("kubevirt")
	if !ok {
		t.Fatal("cueKindDef(kubevirt) reported the kind unknown")
	}
	if err := d.Err(); err != nil {
		t.Fatalf("the contract declares no #ResolvedKubeVirt: %v", err)
	}
}
