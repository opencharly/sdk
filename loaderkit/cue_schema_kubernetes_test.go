package loaderkit

import "testing"

// TestCueKindDefsKubernetesNamesTheContractEnvelope pins the entry opencharly/spec#304 unit 5
// changed. The authored `#Kubernetes` body lives in the plugin that serves the kind now
// (opencharly/plugin-substrate#35), so the contract will stop declaring it; this table reads the
// BASE schema only, so naming the authored def here would panic at first use once that leg lands
// (the exact failure `pod` and `local` each taught). The entry must name the envelope the
// contract DOES keep, and that def must resolve in the embedded schema.
func TestCueKindDefsKubernetesNamesTheContractEnvelope(t *testing.T) {
	if got := cueKindDefs["kubernetes"]; got != "#ResolvedKubernetes" {
		t.Fatalf("cueKindDefs[kubernetes] = %q, want %q — the authored #Kubernetes body is moving out of this contract (spec#304 unit 5)", got, "#ResolvedKubernetes")
	}
	d, ok := cueKindDef("kubernetes")
	if !ok {
		t.Fatal("cueKindDef(kubernetes) reported the kind unknown")
	}
	if err := d.Err(); err != nil {
		t.Fatalf("the contract declares no #ResolvedKubernetes: %v", err)
	}
}
