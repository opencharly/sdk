package loaderkit

import (
	"strings"
	"testing"
)

// TestCueDocFromJSON_IngestAndGate drives the canonical-JSON CUE ingest (parser consolidation
// F2.3) — the validateKindValueCUE gate's JSON twin of CueDocFromYAML: a valid mapping body
// ingests cleanly, a malformed body errors, and a closedness violation surfaces through the
// SAME ValidateEntityClosedCUE gate the load path runs (the gate's exact usage).
func TestCueDocFromJSON_IngestAndGate(t *testing.T) {
	// A valid canonical body ingests cleanly and passes the closedness gate.
	v, err := CueDocFromJSON("node n", []byte(`{"version":"2026.150.0000","description":"x"}`))
	if err != nil {
		t.Fatalf("CueDocFromJSON valid body: %v", err)
	}
	if v.Err() != nil {
		t.Fatalf("CueDocFromJSON built value has an error: %v", v.Err())
	}
	if err := ValidateEntityClosedCUE("box", "node n", v); err != nil {
		t.Fatalf("valid body must pass ValidateEntityClosedCUE: %v", err)
	}

	// A malformed JSON body is a hard ingest error.
	if _, err := CueDocFromJSON("node n", []byte(`{"broken":`)); err == nil {
		t.Fatal("CueDocFromJSON on malformed JSON must error")
	}

	// A closedness-violating body (an unknown field) fails the gate and the error names the field.
	bad, err := CueDocFromJSON("node n", []byte(`{"version":"2026.150.0000","description":"x","zz_unknown_field":1}`))
	if err != nil {
		t.Fatalf("CueDocFromJSON closedness-violating body: %v", err)
	}
	if verr := ValidateEntityClosedCUE("box", "node n", bad); verr == nil {
		t.Fatal("an unknown field must fail ValidateEntityClosedCUE")
	} else if !strings.Contains(verr.Error(), "zz_unknown_field") {
		t.Fatalf("closedness error must name the unknown field: %v", verr)
	}
}
