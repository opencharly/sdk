package sdk

import (
	"testing"
	"testing/fstest"

	"github.com/opencharly/sdk/spec"
)

func TestValidateGeneratedPreservesCUEBytes(t *testing.T) {
	frame := spec.TerminalFrame{
		RunID:    "019f75b2-dfb3-71be-813c-ddc14510f4ca",
		Sequence: 1,
		Kind:     "raw",
		Stream:   "terminal",
		Data:     []byte("typed terminal evidence"),
	}
	if err := ValidateGenerated("#TerminalFrame", frame); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeGeneratedJSONChecksTypedPersistedInputBeforeCUE(t *testing.T) {
	valid := []byte(`{"run_id":"019f75b2-dfb3-71be-813c-ddc14510f4ca","sequence":1,"kind":"status"}`)
	var frame spec.TerminalFrame
	if err := DecodeGeneratedJSON("#TerminalFrame", valid, &frame); err != nil {
		t.Fatal(err)
	}
	withBytes := []byte(`{"run_id":"019f75b2-dfb3-71be-813c-ddc14510f4ca","sequence":1,"kind":"raw","data":"AAEC"}`)
	if err := DecodeGeneratedJSON("#TerminalFrame", withBytes, &frame); err != nil {
		t.Fatal(err)
	}
	if string(frame.Data) != string([]byte{0, 1, 2}) {
		t.Fatalf("decoded bytes = %v", frame.Data)
	}
	if err := DecodeGeneratedJSON("#TerminalFrame", []byte(`{"run_id":"bad","sequence":1,"kind":"status"}`), &frame); err == nil {
		t.Fatal("invalid persisted UUID passed the generated CUE contract")
	}
	if err := DecodeGeneratedJSON("#TerminalFrame", []byte(`{"run_id":"019f75b2-dfb3-71be-813c-ddc14510f4ca","sequence":1,"kind":"unknown"}`), &frame); err == nil {
		t.Fatal("invalid persisted enum passed the generated CUE contract")
	}
	if err := DecodeGeneratedJSON("#TerminalFrame", []byte(`{"run_id":"019f75b2-dfb3-71be-813c-ddc14510f4ca","sequence":1,"kind":"status","unexpected":true}`), &frame); err == nil {
		t.Fatal("unknown persisted field passed the generated CUE contract")
	}
	if err := DecodeGeneratedJSON("#TerminalFrame", []byte(`{"run_id":"019f75b2-dfb3-71be-813c-ddc14510f4ca","sequence":1.5,"kind":"status"}`), &frame); err == nil {
		t.Fatal("fractional persisted integer passed typed decoding")
	}
	if err := DecodeGeneratedJSON("#TerminalFrame", []byte(`{"run_id":"019f75b2-dfb3-71be-813c-ddc14510f4ca","sequence":1,"kind":"raw","data":"%%%"}`), &frame); err == nil {
		t.Fatal("invalid persisted base64 passed typed decoding")
	}
	if err := DecodeGeneratedJSON("#TerminalFrame", append(valid, []byte(` {}`)...), &frame); err == nil {
		t.Fatal("trailing JSON value passed strict decoding")
	}
}

func TestSchemaValidatorPreservesJSONIntegerTypes(t *testing.T) {
	validator, err := NewSchemaValidator(fstest.MapFS{
		"schema/counter.cue": {Data: []byte("#Counter: {count: int & >=0}\n")},
	}, "schema")
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateJSON("#Counter", []byte(`{"count":0}`)); err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateJSON("#Counter", []byte(`{"count":0.5}`)); err == nil {
		t.Fatal("fractional JSON number passed an integer CUE contract")
	}
}
