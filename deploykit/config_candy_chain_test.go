package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// config_candy_chain_test.go — sdk-level coverage for BoxCandyChain / BoxDirectCandies, relocated
// out of charly/candy_chain.go (FLOOR-SLIM Unit 5). Proves the moved code LIVE in the sdk gate.

func testCandyModel(name string) CandyModel {
	return NewSpecCandyModel(spec.CandyModel{}, spec.CandyView{Name: name})
}

func TestBoxCandyChain_InheritsBaseChain(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"base":  spec.EncodeBox(spec.BoxConfig{Candy: []string{"sshd"}}),
			"child": spec.EncodeBox(spec.BoxConfig{Base: "base", Candy: []string{"cdp"}}),
		},
	}
	layers := map[string]CandyModel{
		"sshd": testCandyModel("sshd"),
		"cdp":  testCandyModel("cdp"),
	}
	got, err := BoxCandyChain(cfg, layers, "child")
	if err != nil {
		t.Fatalf("BoxCandyChain() error = %v", err)
	}
	want := []string{"cdp", "sshd"}
	if len(got) != len(want) {
		t.Fatalf("BoxCandyChain(child) = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("BoxCandyChain(child)[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestBoxCandyChain_DedupesFirstOccurrenceWins(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"base":  spec.EncodeBox(spec.BoxConfig{Candy: []string{"sshd"}}),
			"child": spec.EncodeBox(spec.BoxConfig{Base: "base", Candy: []string{"sshd"}}),
		},
	}
	layers := map[string]CandyModel{"sshd": testCandyModel("sshd")}
	got, err := BoxCandyChain(cfg, layers, "child")
	if err != nil {
		t.Fatalf("BoxCandyChain() error = %v", err)
	}
	if len(got) != 1 || got[0] != "sshd" {
		t.Errorf("BoxCandyChain(child) = %v, want [sshd] (deduped, first-occurrence-wins)", got)
	}
}

func TestBoxDirectCandies_NoBaseChainTraversal(t *testing.T) {
	cfg := &spec.Config{
		Box: spec.BoxMap{
			"base":  spec.EncodeBox(spec.BoxConfig{Candy: []string{"sshd"}}),
			"child": spec.EncodeBox(spec.BoxConfig{Base: "base", Candy: []string{"cdp"}}),
		},
	}
	layers := map[string]CandyModel{
		"sshd": testCandyModel("sshd"),
		"cdp":  testCandyModel("cdp"),
	}
	got, err := BoxDirectCandies(cfg, layers, "child")
	if err != nil {
		t.Fatalf("BoxDirectCandies() error = %v", err)
	}
	// Own candies ONLY — "sshd" (from base) must NOT appear.
	if len(got) != 1 || got[0] != "cdp" {
		t.Errorf("BoxDirectCandies(child) = %v, want [cdp] (no base-chain inheritance)", got)
	}
}

func TestBoxDirectCandies_UnknownBoxErrors(t *testing.T) {
	cfg := &spec.Config{Box: spec.BoxMap{}}
	if _, err := BoxDirectCandies(cfg, nil, "nonexistent"); err == nil {
		t.Error("BoxDirectCandies(nonexistent) should error")
	}
}
