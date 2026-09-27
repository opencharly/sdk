package sdk

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/opencharly/spec/spec"
)

// testSchemaFS is the minimal NON-EMPTY CUE schema every plugin MUST now ship.
// The former "stub-gate relaxation" (an input-less plugin could pass a nil
// schemaFS) is DELETED: there is no plugin without a CUE schema, and no
// input-less plugin. These tests exercise the real contract instead of waiving it.
func testSchemaFS() fs.FS {
	return fstest.MapFS{"schema/plugin.cue": &fstest.MapFile{
		Data: []byte("#Input: {\n\tword: string\n}\n"),
	}}
}

// inputlessSchemaFS is a schema FS that concatenates to an EMPTY body (no .cue
// files) — the empty-schema rejection path.
func emptySchemaFS() fs.FS {
	return fstest.MapFS{"schema/README.md": &fstest.MapFile{Data: []byte("not a schema\n")}}
}

// A nil schemaFS is now a hard serve-time error — the input-less waiver is gone.
func TestBuildCapabilitiesRejectsNilSchema(t *testing.T) {
	_, err := BuildCapabilities("2026.176.0001",
		[]ProvidedCapability{{Class: "verb", Word: "x", InputDef: "#Input"}}, nil, "schema")
	if err == nil {
		t.Fatal("BuildCapabilities accepted a nil schemaFS; every plugin MUST ship a CUE schema")
	}
	if !strings.Contains(err.Error(), "NO CUE schema") {
		t.Fatalf("error = %v, want a NO-CUE-schema rejection", err)
	}
}

// A schema FS that concatenates to an empty body is rejected too (a schema dir
// with no .cue files is not a schema).
func TestBuildCapabilitiesRejectsEmptySchema(t *testing.T) {
	_, err := BuildCapabilities("2026.176.0001",
		[]ProvidedCapability{{Class: "verb", Word: "x", InputDef: "#Input"}}, emptySchemaFS(), "schema")
	if err == nil {
		t.Fatal("BuildCapabilities accepted an empty CUE schema")
	}
	if !strings.Contains(err.Error(), "EMPTY CUE schema") {
		t.Fatalf("error = %v, want an EMPTY-CUE-schema rejection", err)
	}
}

// A capability that declares NEITHER an input_def NOR a command model is
// INPUT-LESS and rejected — there is no such thing as an input-less capability,
// command included.
func TestBuildCapabilitiesRejectsInputLessPlugin(t *testing.T) {
	_, err := BuildCapabilities("2026.176.0001",
		[]ProvidedCapability{{Class: "verb", Word: "x"}}, testSchemaFS(), "schema")
	if err == nil {
		t.Fatal("BuildCapabilities accepted an input-less capability (no input_def and no command model)")
	}
	if !strings.Contains(err.Error(), "INPUT-LESS") {
		t.Fatalf("error = %v, want an INPUT-LESS rejection", err)
	}
}

// A class:command capability is NOT input-less when it carries a command model
// (`#CLIModel`) — the uniform typed-input surface for a command. This is the
// shape a trivial command (e.g. `charly version`) ships: a minimal model, never
// nothing.
func TestBuildCapabilitiesAcceptsCommandModelAsTypedInput(t *testing.T) {
	caps, err := BuildCapabilities("2026.176.0001",
		[]ProvidedCapability{{Class: "command", Word: "version", CommandModel: &spec.CLIModel{Name: "version"}}},
		testSchemaFS(), "schema")
	if err != nil {
		t.Fatalf("BuildCapabilities rejected a command with a command model: %v", err)
	}
	provided := caps.GetProvided()
	if len(provided) != 1 || len(provided[0].GetCommandModelJson()) == 0 {
		t.Fatalf("command model was not carried onto the wire: %+v", provided)
	}
}
