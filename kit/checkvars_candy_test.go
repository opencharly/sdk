package kit

// checkvars_candy_test.go — a candy's declared var: must reach the substitution env.
//
// This is the test that fails without the change. The defect it guards: `#Op`'s
// `config:` comment states the rule a candy's variables follow — substitution is
// "candy var: ∪ auto-exports" — and `check:` steps never received that union.
// buildTimeVars read nine BoxMetadata fields and var was not among them, so a bed
// could declare a variable the runner KNEW (its unresolved classifier sees the
// declaration, so the step SKIPs rather than failing DEAD) and could never SUPPLY a
// value for. spec#403 carried var: onto BoxMetadata; this is the sdk leg that reads it.

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestBuildTimeVarsCarryCandyVars pins that a candy's declared var: lands in the map a
// check step's ${NAME} is substituted from. Delete the merge in buildTimeVars and this
// fails on the missing key — which is exactly the SKIP-vs-resolution difference the bed
// reports.
func TestBuildTimeVarsCarryCandyVars(t *testing.T) {
	meta := &spec.BoxMetadata{
		Box:  "some-box",
		User: "someone",
		Vars: map[string]string{
			"CHARLY_PLUGIN_ONLY": "1",
			"CHARLY_PLUGIN_DIR":  "/tmp/noplug",
			"PROBE_FLAG":         "from-the-candy-var",
		},
	}

	env := buildTimeVars(meta, "inst0")

	// The derived defaults still land: this change ADDS to the union, it does not
	// replace it.
	if env["USER"] != "someone" {
		t.Errorf("USER = %q, want someone — the derived defaults must survive the merge", env["USER"])
	}
	if env["IMAGE"] != "some-box" {
		t.Errorf("IMAGE = %q, want some-box", env["IMAGE"])
	}
	if env["INSTANCE"] != "inst0" {
		t.Errorf("INSTANCE = %q, want inst0", env["INSTANCE"])
	}

	// And every declared variable resolves. Before this change none of these three
	// existed in the map, which is why the bed reported them as unresolved.
	for name, want := range meta.Vars {
		if got, ok := env[name]; !ok {
			t.Errorf("%s is not in the substitution env (%v) — a candy that declares a "+
				"variable must have it supplied, not merely known", name, env)
		} else if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestBuildTimeVarsCandyVarWinsOverADerivedDefault pins the precedence: a candy that
// declares a name the builder also derives means its own value, because the declaration
// is authored and the derived value is inferred.
func TestBuildTimeVarsCandyVarWinsOverADerivedDefault(t *testing.T) {
	env := buildTimeVars(&spec.BoxMetadata{User: "derived", Vars: map[string]string{"USER": "declared"}}, "")
	if env["USER"] != "declared" {
		t.Errorf("USER = %q, want declared — an authored candy var: must win over a "+
			"value the builder inferred from another field", env["USER"])
	}
}

// TestBuildTimeVarsNilMetaIsStillEmpty pins that the nil guard did not regress: a
// deployment with no metadata must not panic or invent keys.
func TestBuildTimeVarsNilMetaIsStillEmpty(t *testing.T) {
	if env := buildTimeVars(nil, ""); len(env) != 0 {
		t.Errorf("buildTimeVars(nil) = %v, want an empty map", env)
	}
}
