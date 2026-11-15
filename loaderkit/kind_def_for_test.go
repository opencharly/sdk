package loaderkit

import (
	"testing"

	"cuelang.org/go/cue"

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
	def, ok := kindDefFor("threaded-probe", gates)
	if !ok {
		t.Fatal("kindDefFor did not resolve a def carried by a threaded gate")
	}
	if def.Err() != nil {
		t.Fatalf("resolved def is not usable: %v", def.Err())
	}
	// The def must be a REAL definition from the served source (not an unresolved lookup).
	if !def.Exists() {
		t.Fatal("resolved def does not exist")
	}
	_ = cue.ParsePath // the def path API is exercised by kindDefFor itself

	// The negative control: with no gates, the same word resolves through the BASE — and the base
	// does not declare it, so the lookup must fail rather than silently succeed.
	if _, ok := kindDefFor("threaded-probe", nil); ok {
		t.Fatal("an undeclared kind resolved without a gate — the fallback is not the base for a word nobody declared")
	}
}

// TestKindDefForFallsBackToTheBase pins the other half: a word the CONTRACT still owns resolves
// through cueKindDef, so threading gates never breaks a base-declared kind.
func TestKindDefForFallsBackToTheBase(t *testing.T) {
	if _, ok := kindDefFor("pod", nil); !ok {
		t.Fatal("a base-declared kind stopped resolving when no gates were threaded")
	}
}
