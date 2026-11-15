package deploykit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// candyWithHooks builds a CandyModel (via the existing NewSpecCandyModel envelope
// adapter) carrying only a Hook config — the minimal fixture MergeCandyHooks needs.
func candyWithHooks(h *HooksConfig) CandyModel {
	// The IR carries the hook as OPAQUE BYTES (I1/D-SCH-7), so the fixture marshals the authored
	// shape exactly as loaderkit's producer does — the adapter decodes it back.
	b, err := json.Marshal(h)
	if err != nil {
		panic(err)
	}
	return NewSpecCandyModel(spec.CandyModel{Hook: b}, spec.CandyView{})
}

func TestMergeCandyHooksConcatenatesInOrder(t *testing.T) {
	candies := []CandyModel{
		candyWithHooks(&HooksConfig{PostEnable: "echo first"}),
		candyWithHooks(&HooksConfig{PostEnable: "echo second", PreRemove: "echo cleanup"}),
	}
	got := MergeCandyHooks(candies)
	if got == nil {
		t.Fatal("expected non-nil HooksConfig")
	}
	if want := "echo first\necho second"; got.PostEnable != want {
		t.Errorf("PostEnable = %q, want %q", got.PostEnable, want)
	}
	if want := "echo cleanup"; got.PreRemove != want {
		t.Errorf("PreRemove = %q, want %q", got.PreRemove, want)
	}
}

func TestMergeCandyHooksNoneDeclaredReturnsNil(t *testing.T) {
	candies := []CandyModel{
		candyWithHooks(nil),
		nil,
	}
	if got := MergeCandyHooks(candies); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
