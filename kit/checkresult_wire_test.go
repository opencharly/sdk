package kit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/opencharly/sdk/spec"
)

// checkresult_wire_test.go — the R-FS2 golden parity instrument (FLOOR-SLIM Unit 4, orchestrator
// ruling R-FS2 as amended by the (A) ruling): proves the CheckResult JSON wire-format change is
// EXACTLY the documented key-casing rename (Op→op, Verb→verb, Status→status, Message→message,
// Elapsed→elapsed) — same field SET, same VALUES, same OMISSION behavior — and introduces no
// other drift. The former hand-written CheckResult carried NO json tag on Op/Verb/Status/
// Message/Elapsed (so encoding/json defaulted to the bare, capitalized Go field name, always
// present) and `json:"...,omitempty"` on Attempts/TotalElapsed/CapturedValue (omitted when
// zero); DeadlineExceeded never crossed the wire (`json:"-"`) then OR now.
//
// oldShapeJSON reproduces byte-for-byte what a marshal of the FORMER hand-written type would
// have produced for the same field values (captured from the pre-change source, not derived
// from the new type) — the traceable "before" baseline this test diffs against.
func oldShapeJSON(t *testing.T, cr CheckResult) map[string]any {
	t.Helper()
	// The former hand-written struct, reconstructed verbatim (same field order/tags it had
	// before the FLOOR-SLIM Unit 4 split) so its json.Marshal output is the authoritative
	// "old wire shape" — never hand-typed as a literal string, which would drift from the
	// real former type undetected.
	type oldCheckResult struct {
		Op           *spec.Op
		Verb         string
		Status       Status
		Message      string
		Elapsed      time.Duration
		Attempts     int           `json:"attempts,omitempty"`
		TotalElapsed time.Duration `json:"total_elapsed,omitempty"`
		// DeadlineExceeded bool `json:"-"` — never crossed the wire; omitted here as it would
		// be in the marshal output either way.
		CapturedValue string `json:"captured_value,omitempty"`
	}
	old := oldCheckResult{
		Op:            cr.Op,
		Verb:          cr.Verb,
		Status:        cr.Status,
		Message:       cr.Message,
		Elapsed:       cr.Elapsed,
		Attempts:      cr.Attempts,
		TotalElapsed:  cr.TotalElapsed,
		CapturedValue: cr.CapturedValue,
	}
	b, err := json.Marshal(old)
	if err != nil {
		t.Fatalf("marshal old-shape fixture: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal old-shape fixture: %v", err)
	}
	return m
}

// renameOldKeys projects an old-shape (capitalized, untagged) key set onto the new snake_case
// wire keys — the EXPLICIT, documented rename table (matches the CHANGELOG entry). Any key not
// in this table passes through unchanged (attempts/total_elapsed/captured_value already matched
// before and after).
func renameOldKeys(m map[string]any) map[string]any {
	rename := map[string]string{
		"Op": "op", "Verb": "verb", "Status": "status", "Message": "message", "Elapsed": "elapsed",
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if nk, ok := rename[k]; ok {
			k = nk
		}
		out[k] = v
	}
	return out
}

func newShapeJSON(t *testing.T, cr CheckResult) map[string]any {
	t.Helper()
	b, err := json.Marshal(cr)
	if err != nil {
		t.Fatalf("marshal new-shape CheckResult: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal new-shape CheckResult: %v", err)
	}
	return m
}

// deepEqualJSON reports whether two decoded-JSON maps are equal via a re-marshal/string
// comparison after key-sorting (encoding/json's map marshal sorts keys, so re-marshaling both
// sides gives a stable, order-independent comparison without pulling in reflect.DeepEqual's
// numeric-type footguns across differently-typed map[string]any trees).
func deepEqualJSON(t *testing.T, a, b map[string]any) bool {
	t.Helper()
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("re-marshal a: %v", err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("re-marshal b: %v", err)
	}
	return string(ab) == string(bb)
}

// TestCheckResult_WireParity_Populated proves a fully-populated CheckResult's new JSON shape is
// EXACTLY the old shape with the 5 documented keys renamed — same values, same fields present,
// nothing dropped or added.
func TestCheckResult_WireParity_Populated(t *testing.T) {
	cr := CheckResult{
		CheckResult: spec.CheckResult{
			Op:            &spec.Op{ID: "probe-1"},
			Verb:          "file",
			Status:        StatusFail,
			Message:       "exit=1, want 0",
			Elapsed:       5 * time.Millisecond,
			Attempts:      3,
			TotalElapsed:  15 * time.Millisecond,
			CapturedValue: "xyz",
		},
		DeadlineExceeded: true, // must NEVER appear in either shape's JSON
	}

	oldJSON := renameOldKeys(oldShapeJSON(t, cr))
	newJSON := newShapeJSON(t, cr)

	if !deepEqualJSON(t, oldJSON, newJSON) {
		t.Fatalf("wire parity broken for a populated result — the diff must be EXACTLY key casing:\nold (renamed): %#v\nnew:           %#v", oldJSON, newJSON)
	}
	if _, ok := newJSON["DeadlineExceeded"]; ok {
		t.Error("DeadlineExceeded must NEVER appear in the new-shape JSON (json:\"-\")")
	}
	if _, ok := newJSON["deadline_exceeded"]; ok {
		t.Error("DeadlineExceeded must NEVER appear in the new-shape JSON under any key spelling")
	}
	wantKeys := map[string]bool{"op": true, "verb": true, "status": true, "message": true, "elapsed": true, "attempts": true, "total_elapsed": true, "captured_value": true}
	if len(newJSON) != len(wantKeys) {
		t.Fatalf("new-shape key set = %v, want exactly %v", newJSON, wantKeys)
	}
	for k := range wantKeys {
		if _, ok := newJSON[k]; !ok {
			t.Errorf("new-shape JSON missing expected key %q", k)
		}
	}
}

// TestCheckResult_WireParity_Zero proves the OMISSION behavior is unchanged for a zero-ish
// result: op/verb/status/message/elapsed ALWAYS serialize (no omitempty regression — the exact
// risk the orchestrator's ruling flagged), while attempts/total_elapsed/captured_value are
// omitted exactly as they were before (all zero-valued).
func TestCheckResult_WireParity_Zero(t *testing.T) {
	cr := CheckResult{CheckResult: spec.CheckResult{Status: StatusPass}}

	oldJSON := renameOldKeys(oldShapeJSON(t, cr))
	newJSON := newShapeJSON(t, cr)

	if !deepEqualJSON(t, oldJSON, newJSON) {
		t.Fatalf("wire parity broken for a zero-ish result — the diff must be EXACTLY key casing:\nold (renamed): %#v\nnew:           %#v", oldJSON, newJSON)
	}

	// The always-present fields must survive being zero-valued (no omitempty regression).
	for _, k := range []string{"op", "verb", "status", "message", "elapsed"} {
		if _, ok := newJSON[k]; !ok {
			t.Errorf("required field %q vanished from output at its zero value — an unintended omitempty regression", k)
		}
	}
	// The genuinely-optional fields stay omitted at their zero value, unchanged from before.
	for _, k := range []string{"attempts", "total_elapsed", "captured_value"} {
		if _, ok := newJSON[k]; ok {
			t.Errorf("optional field %q present at its zero value — omitempty behavior changed", k)
		}
	}
}
