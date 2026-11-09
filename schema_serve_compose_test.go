package sdk

// schema_serve_compose_test.go — the sdk's own surface under the module-aware compose.
//
// The composer itself lives in the contract module, `spec/schemacompose` (sdk#490): the host
// may import the contract module and may NOT import the sdk, and both halves of the contract
// must judge the same schema with the same code — so that is where the ONE compose lives.
// What THIS file proves is the surface the sdk owns: `BuildCapabilities` serves a plugin whose
// schema imports the spec package, and keeps serving a plugin that imports nothing. The
// composer's own acceptance arms (the imported definition enforced, equality with the deleted
// text compile, the two entry points agreeing byte for byte) live beside it, in
// `spec/schemacompose`'s own tests.

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// restatingPluginFS is a plugin schema in the pre-import shape: it RESTATES the spec
// definitions it needs (the state every consumer is in until the per-consumer units land)
// and imports nothing.
func restatingPluginFS() fs.FS {
	return fstest.MapFS{"schema/tunnel.cue": &fstest.MapFile{Data: []byte(`#TunnelConfig: {
	provider?: string
	ports?: [...#TunnelPort]
}

#TunnelPort: {
	port?: int
	protocol?: string
}

#TunnelInput: #TunnelConfig & {
	provider: "tailscale" | "cloudflare"
}
`)}}
}

// importingPluginFS is a plugin schema in the shape the cutover exists for: it imports
// `github.com/opencharly/spec/schema` and cites the definition spec owns instead of
// restating it. It carries NO cue.mod of its own, so it is composed under the derived
// declaration — the same declaration the host derives from the same source text.
func importingPluginFS() fs.FS {
	return fstest.MapFS{"schema/tunnel.cue": &fstest.MapFile{Data: []byte(`import "github.com/opencharly/spec/schema"

#TunnelInput: schema.#TunnelConfig & {
	provider: "tailscale" | "cloudflare"
}
`)}}
}

// TestBuildCapabilitiesServesAnImportingSchema proves the PUBLIC serve surface carries a
// plugin that imports spec at all. Fails before the cutover: BuildCapabilities reported
// "plugin schema does not compile: package github.com/opencharly/spec/schema imported but not
// defined", so such a plugin could not start. The wire body stays the package-less text (the
// host composes it with the same composer), so the import clause must survive intact.
func TestBuildCapabilitiesServesAnImportingSchema(t *testing.T) {
	caps, err := BuildCapabilities("2026.313.0209",
		[]ProvidedCapability{{Class: "verb", Word: "tunnel", InputDef: "#TunnelInput"}},
		importingPluginFS(), "schema")
	if err != nil {
		t.Fatalf("BuildCapabilities must accept a plugin schema that imports spec: %v", err)
	}
	if !strings.Contains(caps.GetSchemaCue(), `import "github.com/opencharly/spec/schema"`) {
		t.Fatalf("the served schema_cue must carry the plugin's import clause; got:\n%s", caps.GetSchemaCue())
	}
	if strings.Contains(caps.GetSchemaCue(), "package ") {
		t.Fatalf("the served schema_cue must stay package-less (the host re-establishes the package); got:\n%s", caps.GetSchemaCue())
	}
}

// TestBuildCapabilitiesServesARestatingSchema is the other half of the same contract: a plugin
// that imports nothing still serves, and what it serves is its own schema text unchanged. The
// compose must not rewrite a consumer's source on the way to the wire — the host composes that
// same text, so a rewrite here would be a second schema.
func TestBuildCapabilitiesServesARestatingSchema(t *testing.T) {
	caps, err := BuildCapabilities("2026.313.0209",
		[]ProvidedCapability{{Class: "verb", Word: "tunnel", InputDef: "#TunnelInput"}},
		restatingPluginFS(), "schema")
	if err != nil {
		t.Fatalf("BuildCapabilities must serve a plugin schema that imports nothing: %v", err)
	}
	if strings.Contains(caps.GetSchemaCue(), "import ") {
		t.Fatalf("a restating plugin's served body gained an import clause; got:\n%s", caps.GetSchemaCue())
	}
	for _, want := range []string{"#TunnelConfig:", "#TunnelPort:", "#TunnelInput:"} {
		if !strings.Contains(caps.GetSchemaCue(), want) {
			t.Fatalf("the served body lost %q; got:\n%s", want, caps.GetSchemaCue())
		}
	}
}
