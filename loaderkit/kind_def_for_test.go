package loaderkit

import (
	"cuelang.org/go/cue/cuecontext"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestKindDefForResolvesAThreadedGate pins the gate branch: a kind whose authored body lives in a
// plugin is reachable HERE from the served source alone, with no registry and no base declaration.
// Without this branch kindDefFor degenerates to cueKindDef and the whole suite would pass
// identically against the pre-cutover resolution.
func TestKindDefForResolvesAThreadedGate(t *testing.T) {
	const src = "package schema\n#ThreadedProbe: {firmware!: *\"bios\" | string}"
	gates := map[string]spec.KindGate{
		"threaded-probe": {Def: "#ThreadedProbe", Source: src},
	}

	// The gate branch: the def resolves from the SERVED source, even though the base schema has
	// never heard of this kind.
	def, derr := kindDefFor("threaded-probe", gates)
	if derr != nil {
		t.Fatalf("kindDefFor did not resolve a def carried by a threaded gate: %v", derr)
	}
	if def.Err() != nil {
		t.Fatalf("resolved def is not usable: %v", def.Err())
	}
	// The def must be a REAL definition from the served source (not an unresolved lookup).
	if !def.Exists() {
		t.Fatal("resolved def does not exist")
	}

	// The negative control: with no gates, the same word resolves through the BASE — and the base
	// does not declare it, so the lookup must fail rather than silently succeed.
	if _, err := kindDefFor("threaded-probe", nil); err == nil {
		t.Fatal("an undeclared kind resolved without a gate — the base is not an authority for a word nobody declared")
	}
}

// TestKindDefForFallsBackToTheBase pins the other half: a word the CONTRACT still owns resolves
// through cueKindDef, so threading gates never breaks a base-declared kind.
func TestKindDefForFallsBackToTheBase(t *testing.T) {
	if _, err := kindDefFor("pod", nil); err != nil {
		t.Fatalf("a base-declared kind stopped resolving when no gates were threaded: %v", err)
	}
}

// TestKindDefForSurfacesABrokenGate pins the authority rule: a DECLARED gate that cannot compile
// or whose def is absent is an ERROR, never a silent fall-through to the base - which would report
// "no CUE schema registered" for a body that HAS a declaration.
func TestKindDefForSurfacesABrokenGate(t *testing.T) {
	broken := map[string]spec.KindGate{
		"threaded-probe": {Def: "#ThreadedProbe", Source: "package schema\n#ThreadedProbe: {unclosed"},
	}
	if _, err := kindDefFor("threaded-probe", broken); err == nil {
		t.Fatal("a gate whose source does not compile fell back to the base instead of failing")
	}
	missingDef := map[string]spec.KindGate{
		"threaded-probe": {Def: "#NotDeclared", Source: "package schema\n#ThreadedProbe: {firmware!: *\"bios\" | string}"},
	}
	if _, err := kindDefFor("threaded-probe", missingDef); err == nil {
		t.Fatal("a gate whose def is absent fell back to the base instead of failing")
	}
}

// TestKindDefForCompilesAGateThatImportsTheSchemaModule pins opencharly/opencharly#1043: a served
// plugin schema may IMPORT the spec schema module, and a gate source must still resolve. Before
// this, kindDefFor prepended the concatenated base to a bare CompileString, which has no module
// context — the import resolved to nothing and every such gate failed with "imported but not
// defined", so a body that had correctly moved into a plugin became unresolvable.
func TestKindDefForCompilesAGateThatImportsTheSchemaModule(t *testing.T) {
	// A def that CITES the base schema through the module import: the whole point is that the
	// citation must resolve, which only a module-aware compile can do.
	const src = `package schema

import "github.com/opencharly/spec/schema"

#GateProbe: {
	firmware!: *"bios" | string
	step?: schema.#Step
}
`
	gates := map[string]spec.KindGate{"gate-probe": {Def: "#GateProbe", Source: src}}
	d, err := kindDefFor("gate-probe", gates)
	if err != nil {
		t.Fatalf("a gate that imports the schema module must compile: %v", err)
	}
	if d.Err() != nil {
		t.Fatalf("the resolved def is invalid: %v", d.Err())
	}
}

// TestValidateEntityCUEWithGatesResolvesTheAuthoredBody pins opencharly/opencharly#1048's second
// consumer: entity validation asked the BASE table for the kind's def, and for a kind whose
// authored body moved into a plugin that entry is whatever keeper remained (`vm` -> `#ResolvedVm`,
// the POST-default envelope), which an authored document can never satisfy. With the gates
// threaded, the authored def resolves; with the base-table path (nil gates) the same entity is
// refused. The pair is the coverage: it fails if the gated resolution stops being used.
func TestValidateEntityCUEWithGatesResolvesTheAuthoredBody(t *testing.T) {
	ctx := cuecontext.New()
	const src = "package schema\n#GateProbe: {firmware: *\"bios\" | string}"
	// An AUTHORED fixture: it omits nothing the authored def requires, and it would fail against a
	// post-default envelope that made every scalar mandatory.
	entity := ctx.CompileString("firmware: \"bios\"\n")

	gates := map[string]spec.KindGate{"gate-probe": {Def: "#GateProbe", Source: src}}
	if err := ValidateEntityCUEWithGates("gate-probe", "t.yml:gate-probe", entity, gates); err != nil {
		t.Fatalf("the gated variant must resolve the authored def: %v", err)
	}
	// The control: without the gates there is no such kind in the base, so the same entity must
	// FAIL — a silent pass here would mean the gates were ignored.
	if err := ValidateEntityCUEWithGates("gate-probe", "t.yml:gate-probe", entity, nil); err == nil {
		t.Fatal("an undeclared kind validated without its gate — the gates are ignored")
	}
}
