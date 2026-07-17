package deploykit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// candyWithHooks builds a CandyModel (via the existing NewSpecCandyModel envelope
// adapter) carrying only a Hook config — the minimal fixture MergeCandyHooks needs.
func candyWithHooks(h *HooksConfig) CandyModel {
	return NewSpecCandyModel(spec.CandyModel{Hook: h}, spec.CandyView{})
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
