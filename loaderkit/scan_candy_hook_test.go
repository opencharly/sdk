package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestPopulateFromYAMLWritesTheHookAsJSON pins the PRODUCER half of the byte crossing, exercising
// the real populateFromYAML rather than a fixture that re-implements it — a regression here would
// otherwise reach production with every test still green.
//
// The IR carries a candy's hook as OPAQUE BYTES (DESIGN.md 3.1, I1/D-SCH-7). The producer must
// therefore write exactly the JSON the authored #CandyHook serialises to, because that is the shape
// the OCI label and the build engine both decode.
func TestPopulateFromYAMLWritesTheHookAsJSON(t *testing.T) {
	t.Run("authored hook becomes its JSON bytes", func(t *testing.T) {
		m := &spec.CandyModel{}
		ly := &spec.CandyYAML{
			Name: "demo",
			Hook: &spec.CandyHook{PostEnable: "echo up", PreRemove: "echo down"},
		}
		populateFromYAML(m, &spec.CandyView{}, ly)

		if len(m.Hook) == 0 {
			t.Fatal("m.Hook is empty — the producer dropped the authored hook")
		}
		var round spec.CandyHook
		if err := json.Unmarshal(m.Hook, &round); err != nil {
			t.Fatalf("m.Hook is not decodable JSON (%q): %v", m.Hook, err)
		}
		if round.PostEnable != "echo up" || round.PreRemove != "echo down" {
			t.Fatalf("round-tripped hook = %+v, want the authored values", round)
		}
	})

	t.Run("no authored hook leaves the body empty", func(t *testing.T) {
		m := &spec.CandyModel{}
		populateFromYAML(m, &spec.CandyView{}, &spec.CandyYAML{Name: "demo"})
		if len(m.Hook) != 0 {
			t.Fatalf("m.Hook = %q for a candy with no hook, want empty", m.Hook)
		}
	})
}
