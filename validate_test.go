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
