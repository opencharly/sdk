package sdk

// schema_compose_test.go — the acceptance tests for sdk#490's composer.
//
// WHAT THIS FILE PROVES, and why each arm would pass without the fix:
//
//   - TestComposeServedSchemaEnforcesAnImportedSpecDefinition — a plugin whose schema
//     IMPORTS `github.com/opencharly/spec/schema` composes, and the imported definition is
//     LIVE (a violating document fails with the definition's own constraint error, a
//     conforming one passes). This is the R7 arm: on the pre-fix path the compose fails at
//     the import ("package ... imported but not defined"), so the test cannot pass before
//     the change.
//   - TestComposeServedSchemaMatchesTheLegacyTextCompile — the REGRESSION GUARD: a plugin
//     that does NOT import (it restates the defs, as every consumer does today) composes to
//     exactly the value the deleted `CompileString` path produced, and judges the same
//     documents the same way. It guards the fix against changing what a self-contained
//     plugin means.
//   - TestComposeServedTextMatchesComposeServedSchema — the two halves of the contract agree
//     byte for byte: the serve side composes the plugin's FS, the host composes the
//     package-less body that reached it over the wire, and the two values render identically
//     and accept/reject the same documents. Without the fix the host half cannot compose an
//     importing unit at all.

import (
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"

	"github.com/opencharly/spec/schemaconcat"
)

// servedInputDef is the one input def every fixture below declares.
const servedInputDef = "#TunnelInput"

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

// renderServedValue renders a composed schema as canonical CUE. A cue.Value is only valid
// inside the context that built it, so a value is compared by what it SAYS (its CUE
// rendering) and by how it JUDGES documents — never by pointer identity.
func renderServedValue(t *testing.T, v cue.Value) string {
	t.Helper()
	n := v.Syntax(
		cue.Final(),
		cue.Concrete(false),
		cue.Definitions(true),
		cue.Hidden(true),
		cue.Optional(true),
		cue.Attributes(true),
		cue.Docs(false),
	)
	data, err := format.Node(n)
	if err != nil {
		t.Fatalf("rendering the composed schema: %v", err)
	}
	return string(data)
}

// judgeServedDoc unifies a document with a def of a composed schema and returns the verdict
// as a string ("<nil>" for a document the schema accepts, the error text otherwise).
func judgeServedDoc(t *testing.T, schema cue.Value, doc string) string {
	t.Helper()
	d := schema.LookupPath(cue.ParsePath(servedInputDef))
	if d.Err() != nil {
		t.Fatalf("composed schema has no %s: %v", servedInputDef, d.Err())
	}
	ctx := cuecontext.New()
	in := ctx.CompileString(doc)
	if in.Err() != nil {
		t.Fatalf("test document does not compile: %v", in.Err())
	}
	if err := d.Unify(in).Validate(cue.Concrete(true)); err != nil {
		return err.Error()
	}
	return "<nil>"
}

// TestComposeServedSchemaEnforcesAnImportedSpecDefinition is the R7 arm: the composed
// schema with a plugin that IMPORTS spec must compile, and the imported definition must be
// ENFORCED — not merely accepted. Fails before the change: the text compile refuses the
// import ("package github.com/opencharly/spec/schema imported but not defined").
func TestComposeServedSchemaEnforcesAnImportedSpecDefinition(t *testing.T) {
	v, err := ComposeServedSchema(nil, importingPluginFS(), "schema")
	if err != nil {
		t.Fatalf("a plugin that imports the spec schema must compose: %v", err)
	}

	// The imported definition is live: `provider` is spec's `provider?: string`, so an int is
	// the imported constraint's own error, reported against the def the plugin cites.
	const violating = `{"provider": 5}`
	if got := judgeServedDoc(t, v, violating); !strings.Contains(got, "provider") || !strings.Contains(got, "string") {
		t.Fatalf("a document violating the IMPORTED spec def must fail naming provider/string; got: %s", got)
	} else {
		t.Logf("violating spec.#TunnelConfig -> %s", got)
	}

	// A field ONLY spec's definition declares (`#TunnelPort.port?: int`) proves the imported
	// shape travelled too, not just the one constrained field.
	if got := judgeServedDoc(t, v, `{"ports": [{"port": "eighty"}]}`); !strings.Contains(got, "port") || got == "<nil>" {
		t.Fatalf("a document violating the imported #TunnelPort.port must fail; got: %s", got)
	} else {
		t.Logf("violating spec.#TunnelPort -> %s", got)
	}

	const conforming = `{"provider": "tailscale", "ports": [{"port": 8080, "protocol": "https"}]}`
	if got := judgeServedDoc(t, v, conforming); got != "<nil>" {
		t.Fatalf("a document conforming to the imported spec def must pass; got: %s", got)
	} else {
		t.Logf("conforming spec.#TunnelConfig -> err = %s", got)
	}
}

// TestComposeServedSchemaMatchesTheLegacyTextCompile is the REGRESSION GUARD: a plugin that
// does NOT import must compose to the value the deleted `CompileString` path produced.
// "The same result" is asserted two ways — the canonical CUE rendering is identical, and the
// two values judge a corpus of documents (conforming and violating) identically.
func TestComposeServedSchemaMatchesTheLegacyTextCompile(t *testing.T) {
	fsys := restatingPluginFS()

	body, _, err := schemaconcat.ConcatSchema(fsys, "schema", nil)
	if err != nil {
		t.Fatalf("concat: %v", err)
	}
	legacy := cuecontext.New().CompileString(body)
	if legacy.Err() != nil {
		t.Fatalf("the legacy text compile must accept a self-contained plugin schema: %v", legacy.Err())
	}

	composed, err := ComposeServedSchema(nil, fsys, "schema")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	legacyRendered := renderServedValue(t, legacy)
	composedRendered := renderServedValue(t, composed)
	if legacyRendered != composedRendered {
		t.Fatalf("a non-importing plugin must compose to the SAME result as the legacy text compile:\n--- legacy ---\n%s\n--- composed ---\n%s", legacyRendered, composedRendered)
	}

	for _, doc := range []string{
		`{"provider": "tailscale", "ports": [{"port": 8080}]}`,
		`{"provider": 5}`,
		`{"ports": [{"port": "eighty"}]}`,
		`{"provider": "nope"}`,
		`{}`,
	} {
		legacyVerdict := judgeServedDoc(t, legacy, doc)
		composedVerdict := judgeServedDoc(t, composed, doc)
		if legacyVerdict != composedVerdict {
			t.Fatalf("document %s judged differently after the cutover:\n legacy: %s\n composed: %s", doc, legacyVerdict, composedVerdict)
		}
	}
}

// TestComposeServedTextMatchesComposeServedSchema proves the two halves of the contract
// agree byte for byte. The serve side holds the plugin's FS; the host holds only the
// package-less `.cue` body that travelled on `Capabilities.schema_cue`. Both must compose
// the SAME schema — for an importing plugin and for a restating one.
func TestComposeServedTextMatchesComposeServedSchema(t *testing.T) {
	for name, fsys := range map[string]fs.FS{
		"restating": restatingPluginFS(),
		"importing": importingPluginFS(),
	} {
		t.Run(name, func(t *testing.T) {
			fromFS, err := ComposeServedSchema(nil, fsys, "schema")
			if err != nil {
				t.Fatalf("serve-side compose: %v", err)
			}
			body, _, err := schemaconcat.ConcatSchema(fsys, "schema", nil)
			if err != nil {
				t.Fatalf("concat: %v", err)
			}
			fromWire, err := ComposeServedText(nil, body)
			if err != nil {
				t.Fatalf("host-side compose: %v", err)
			}
			want, got := renderServedValue(t, fromFS), renderServedValue(t, fromWire)
			if want != got {
				t.Fatalf("serve-side and host-side must compose the same schema:\n--- serve ---\n%s\n--- host ---\n%s", want, got)
			}
			for _, doc := range []string{`{"provider": "tailscale"}`, `{"provider": 5}`, `{}`} {
				if a, b := judgeServedDoc(t, fromFS, doc), judgeServedDoc(t, fromWire, doc); a != b {
					t.Fatalf("document %s judged differently by the two halves:\n serve: %s\n host: %s", doc, a, b)
				}
			}
		})
	}
}

// TestBuildCapabilitiesServesAnImportingSchema proves the PUBLIC serve surface changed: a
// plugin that imports spec reaches the wire at all. Before the fix BuildCapabilities failed
// with "plugin schema does not compile: package ... imported but not defined", so a consumer
// could not start. The wire body stays the package-less text (the host composes it with the
// same composer), so the import clause must survive intact.
func TestBuildCapabilitiesServesAnImportingSchema(t *testing.T) {
	caps, err := BuildCapabilities("2026.313.0209",
		[]ProvidedCapability{{Class: "verb", Word: "tunnel", InputDef: servedInputDef}},
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

// TestComposeServedSchemaUsesThePluginsOwnModule proves the composer reads a plugin's own
// `cue.mod/module.cue` when its FS ships one — the file the per-consumer unit adds. The
// plugin here declares its own module path and (identically) the spec dependency, and the
// composed value is the same one the derived declaration produces, because a module path is
// an identity, not a value.
func TestComposeServedSchemaUsesThePluginsOwnModule(t *testing.T) {
	withModule := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{Data: []byte(`module: "github.com/opencharly/plugin-tunnel"

language: {
	version: "v0.16.1"
}

deps: {
	"github.com/opencharly/spec": v: "v0.0.0"
}
`)},
		"schema/tunnel.cue": &fstest.MapFile{Data: []byte(`import "github.com/opencharly/spec/schema"

#TunnelInput: schema.#TunnelConfig & {
	provider: "tailscale" | "cloudflare"
}
`)},
	}

	own, err := ComposeServedSchema(nil, withModule, "schema")
	if err != nil {
		t.Fatalf("a plugin that ships its own cue.mod must compose: %v", err)
	}
	derived, err := ComposeServedSchema(nil, importingPluginFS(), "schema")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if a, b := renderServedValue(t, own), renderServedValue(t, derived); a != b {
		t.Fatalf("a plugin's own module declaration must not change the composed value:\n--- own module ---\n%s\n--- derived module ---\n%s", a, b)
	}
}

// TestComposeServedSchemaRejectsAForeignLanguageVersion pins the ONE consistency check the
// plugin's own declaration must pass: the host composes the same source text under the
// derived declaration (the wire carries no module), so a plugin evaluated under another
// language version would be judged by two different semantics on the two sides.
func TestComposeServedSchemaRejectsAForeignLanguageVersion(t *testing.T) {
	foreign := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{Data: []byte(`module: "github.com/opencharly/plugin-tunnel"

language: {
	version: "v0.17.1"
}
`)},
		"schema/tunnel.cue": &fstest.MapFile{Data: []byte("#TunnelInput: {provider?: string}\n")},
	}
	_, err := ComposeServedSchema(nil, foreign, "schema")
	if err == nil {
		t.Fatal("a plugin module declaring a different language.version than the spec module must be refused")
	}
	for _, want := range []string{"v0.17.1", "v0.16.1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name both versions (%q); got: %v", want, err)
		}
	}
}

// TestComposeServedSchemaIsDeterministic proves the materialized spec module and the compose
// are stable: the same input composes to the same value on repeated calls, and a broken
// schema still reports the failure (the compose is not silently tolerant).
func TestComposeServedSchemaIsDeterministic(t *testing.T) {
	first, err := ComposeServedSchema(nil, importingPluginFS(), "schema")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	second, err := ComposeServedSchema(nil, importingPluginFS(), "schema")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if a, b := renderServedValue(t, first), renderServedValue(t, second); a != b {
		t.Fatalf("two composes of the same input differ:\n--- first ---\n%s\n--- second ---\n%s", a, b)
	}

	broken := fstest.MapFS{"schema/broken.cue": &fstest.MapFile{Data: []byte("#Broken: {\n\tx: nosuchfield.y\n}\n")}}
	if _, err := ComposeServedSchema(nil, broken, "schema"); err == nil {
		t.Fatal("a schema that does not compile must be refused by the compose")
	} else if !strings.Contains(err.Error(), "nosuchfield") {
		t.Fatalf("the compose error must name the unresolved reference; got: %v", err)
	}
}

// TestComposeServedSchemaRefusesAnUnservableDependency pins the registry's boundary: the
// compose serves the spec module and nothing else, so a plugin that imports a module nobody
// can resolve offline fails loudly HERE rather than resolving against a toolchain default.
func TestComposeServedSchemaRefusesAnUnservableDependency(t *testing.T) {
	fsys := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{Data: []byte(`module: "github.com/opencharly/plugin-tunnel"

language: {
	version: "v0.16.1"
}

deps: {
	"example.com/elsewhere": v: "v1.0.0"
}
`)},
		"schema/x.cue": &fstest.MapFile{Data: []byte("import \"example.com/elsewhere/pkg\"\n\n#X: {a?: string}\n")},
	}
	_, err := ComposeServedSchema(nil, fsys, "schema")
	if err == nil {
		t.Fatal("a plugin importing a module the offline registry does not serve must fail loudly")
	}
	t.Logf("unservable dependency -> %v", err)
	// The failure names the module nobody can resolve, never a nil value a caller could go on
	// with.
	if !strings.Contains(err.Error(), "example.com/elsewhere") {
		t.Fatalf("the refusal must name the unresolvable module; got: %v", err)
	}
}

// TestComposeServedSchemaConcurrent proves the materialized spec module and the compose are
// safe under concurrent use: the host composes every unit of a plugin set, and a compose of
// the same schema must never depend on another compose's timing. Run with -race, this is the
// arm that catches a shared file-system or loader cache.
func TestComposeServedSchemaConcurrent(t *testing.T) {
	const workers = 8
	rendered := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := ComposeServedSchema(nil, importingPluginFS(), "schema")
			if err != nil {
				errs[i] = err
				return
			}
			n := v.Syntax(cue.Final(), cue.Concrete(false), cue.Definitions(true), cue.Hidden(true), cue.Optional(true), cue.Attributes(true), cue.Docs(false))
			data, err := format.Node(n)
			if err != nil {
				errs[i] = err
				return
			}
			rendered[i] = string(data)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	for i := 1; i < workers; i++ {
		if rendered[i] != rendered[0] {
			t.Fatalf("worker %d composed a different schema than worker 0", i)
		}
	}
}
