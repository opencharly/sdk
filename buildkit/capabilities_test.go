package buildkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// capabilities_test.go — coverage for the candy capabilities aggregator relocated to
// sdk/buildkit. Uses fakeCandyReader (candy_reader_fake_test.go) as the
// spec.CandyReader test double CLAUDE.md calls for; each case would fail if the
// aggregation, conflict-detection, or missing-capability logic regressed.

func TestAggregateCandyCapabilities(t *testing.T) {
	t.Run("merges booleans and oci labels across candies in order", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{name: "a", caps: &spec.CandyCapability{
				PreserveUser: true,
				OCILabels:    map[string]string{"a.label": "1"},
			}},
			"b": &fakeCandyReader{name: "b", caps: &spec.CandyCapability{
				NeedsRootAfterInit: true,
				DataOnly:           true,
				InitSystemHint:     "systemd",
				OCILabels:          map[string]string{"b.label": "2"},
			}},
		}
		agg, err := AggregateCandyCapabilities(layers, []string{"a", "b"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !agg.PreserveUser || !agg.NeedsRootAfterInit || !agg.DataOnly {
			t.Errorf("expected all three booleans set, got %+v", agg)
		}
		if agg.InitSystemHint != "systemd" {
			t.Errorf("InitSystemHint = %q, want systemd", agg.InitSystemHint)
		}
		if agg.OCILabels["a.label"] != "1" || agg.OCILabels["b.label"] != "2" {
			t.Errorf("OCILabels merge incomplete: %+v", agg.OCILabels)
		}
		if !agg.Provided["preserve_user"] || !agg.Provided["needs_root_after_init"] ||
			!agg.Provided["data_only"] || !agg.Provided["init_system:systemd"] {
			t.Errorf("Provided set incomplete: %+v", agg.Provided)
		}
	})

	t.Run("nil/missing layers in order are skipped, not a panic", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"present": &fakeCandyReader{caps: &spec.CandyCapability{PreserveUser: true}},
		}
		agg, err := AggregateCandyCapabilities(layers, []string{"missing", "present"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !agg.PreserveUser {
			t.Errorf("expected PreserveUser true from the present layer, got %+v", agg)
		}
	})

	t.Run("nil Capabilities() on a layer contributes nothing", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"noop": &fakeCandyReader{caps: nil},
		}
		agg, err := AggregateCandyCapabilities(layers, []string{"noop"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if agg.PreserveUser || agg.NeedsRootAfterInit || agg.DataOnly || len(agg.OCILabels) != 0 {
			t.Errorf("expected zero-value aggregate, got %+v", agg)
		}
	})

	t.Run("conflicting oci_label values across layers is an error naming both layers", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"first":  &fakeCandyReader{caps: &spec.CandyCapability{OCILabels: map[string]string{"k": "v1"}}},
			"second": &fakeCandyReader{caps: &spec.CandyCapability{OCILabels: map[string]string{"k": "v2"}}},
		}
		_, err := AggregateCandyCapabilities(layers, []string{"first", "second"})
		if err == nil {
			t.Fatal("expected a conflict error, got nil")
		}
		msg := err.Error()
		for _, want := range []string{"k", "v1", "v2", "first", "second"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q missing expected substring %q", msg, want)
			}
		}
	})

	t.Run("same value for the same key across layers is not a conflict", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"first":  &fakeCandyReader{caps: &spec.CandyCapability{OCILabels: map[string]string{"k": "same"}}},
			"second": &fakeCandyReader{caps: &spec.CandyCapability{OCILabels: map[string]string{"k": "same"}}},
		}
		agg, err := AggregateCandyCapabilities(layers, []string{"first", "second"})
		if err != nil {
			t.Fatalf("unexpected error for identical values: %v", err)
		}
		if agg.OCILabels["k"] != "same" {
			t.Errorf("OCILabels[k] = %q, want same", agg.OCILabels["k"])
		}
	})
}

func TestCheckRequiredCapabilities(t *testing.T) {
	t.Run("no requirements means no missing", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{},
		}
		got := CheckRequiredCapabilities(layers, []string{"a"}, &AggregatedCandyCaps{Provided: map[string]bool{}})
		if len(got) != 0 {
			t.Errorf("expected no missing capabilities, got %v", got)
		}
	})

	t.Run("a required capability nobody provides is reported, sorted", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{requiresCaps: []string{"gpu", "tpm"}},
			"b": &fakeCandyReader{requiresCaps: []string{"gpu"}},
		}
		got := CheckRequiredCapabilities(layers, []string{"a", "b"}, &AggregatedCandyCaps{Provided: map[string]bool{}})
		want := []string{"gpu", "tpm"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("CheckRequiredCapabilities = %v, want %v", got, want)
		}
	})

	t.Run("a provided capability is not reported missing", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{requiresCaps: []string{"gpu"}},
		}
		agg := &AggregatedCandyCaps{Provided: map[string]bool{"gpu": true}}
		got := CheckRequiredCapabilities(layers, []string{"a"}, agg)
		if len(got) != 0 {
			t.Errorf("expected gpu to be satisfied, got missing=%v", got)
		}
	})

	t.Run("nil agg is treated as empty Provided", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{requiresCaps: []string{"gpu"}},
		}
		got := CheckRequiredCapabilities(layers, []string{"a"}, nil)
		if len(got) != 1 || got[0] != "gpu" {
			t.Errorf("expected [gpu] missing with nil agg, got %v", got)
		}
	})
}

func TestCandyCapabilitiesError(t *testing.T) {
	t.Run("empty missing list returns nil error", func(t *testing.T) {
		layers := map[string]spec.CandyReader{"a": &fakeCandyReader{}}
		if err := CandyCapabilitiesError(layers, []string{"a"}, nil); err != nil {
			t.Errorf("expected nil error for empty missing list, got %v", err)
		}
	})

	t.Run("formats requester names + requested capability for each missing entry", func(t *testing.T) {
		layers := map[string]spec.CandyReader{
			"gpu-app": &fakeCandyReader{requiresCaps: []string{"gpu"}},
			"tpm-app": &fakeCandyReader{requiresCaps: []string{"tpm"}},
			"neither": &fakeCandyReader{},
		}
		err := CandyCapabilitiesError(layers, []string{"gpu-app", "tpm-app", "neither"}, []string{"gpu", "tpm"})
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		msg := err.Error()
		for _, want := range []string{
			"gpu", "tpm",
			"gpu-app (requires gpu)",
			"tpm-app (requires tpm)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q missing expected substring %q", msg, want)
			}
		}
		if strings.Contains(msg, "neither (requires") {
			t.Errorf("error %q should not list the candy that requires nothing", msg)
		}
	})
}
