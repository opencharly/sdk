package loaderkit

import (
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
