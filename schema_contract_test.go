package sdk

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// testSchemaFS is the minimal NON-EMPTY CUE schema every plugin MUST now ship.
// The former "stub-gate relaxation" (an input-less plugin could pass a nil
// schemaFS) is DELETED: there is no plugin without a CUE schema. These tests
// exercise the real contract instead of waiving it.
func testSchemaFS() fs.FS {
	return fstest.MapFS{"schema/plugin.cue": &fstest.MapFile{
		Data: []byte("#Input: {\n\tword: string\n}\n"),
	}}
}

// emptySchemaFS is a schema FS that concatenates to an EMPTY body (no .cue
// files) — the empty-schema rejection path.
func emptySchemaFS() fs.FS {
	return fstest.MapFS{"schema/README.md": &fstest.MapFile{Data: []byte("not a schema\n")}}
}

// A nil schemaFS is now a hard serve-time error — the schema waiver is gone.
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

// A capability that declares no structured input (a pass-through command, a
// substrate kind, a deploy target) is NOT a schema exemption: the plugin still
// ships its "doc schema" and serves fine. This pins the uniform rule — the
// schema is required for EVERY plugin, while a per-capability InputDef is not.
func TestBuildCapabilitiesAcceptsDocSchemaWithoutInputDef(t *testing.T) {
	caps, err := BuildCapabilities("2026.176.0001",
		[]ProvidedCapability{
			{Class: "command", Word: "review"},
			{Class: "kind", Word: "candy"},
			{Class: "deploy", Word: "local"},
		}, testSchemaFS(), "schema")
	if err != nil {
		t.Fatalf("BuildCapabilities rejected a plugin that ships a doc schema: %v", err)
	}
	if got := len(caps.GetProvided()); got != 3 {
		t.Fatalf("provided length = %d, want 3", got)
	}
}
