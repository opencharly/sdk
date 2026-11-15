package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestPopulateFromYAMLWritesExtractAsJSON is the producer half of the extract crossing, the twin of
// TestPopulateFromYAMLWritesTheHookAsJSON — and the test the review was right to require.
//
// The IR carries a candy's extractions as OPAQUE BYTES (DESIGN.md 3.1, I1/D-SCH-7). The producer
// must therefore write exactly the JSON the authored #CandyExtract list serialises to. A fixture
// that marshals locally cannot prove this: it exercises the fixture, not populateFromYAML, which is
// the code a regression would break.
func TestPopulateFromYAMLWritesExtractAsJSON(t *testing.T) {
	t.Run("authored extractions become their JSON bytes", func(t *testing.T) {
		m := &spec.CandyModel{}
		ly := &spec.CandyYAML{
			Name: "demo",
			Extract: []spec.CandyExtract{
				{Source: "builder", Path: "/out/one", Dest: "/usr/one"},
				{Source: "builder", Path: "/out/two", Dest: "/usr/two"},
			},
		}
		populateFromYAML(m, &spec.CandyView{}, ly)

		if len(m.Extract) == 0 {
			t.Fatal("m.Extract is empty — the producer dropped the authored extractions")
		}
		var round []spec.CandyExtract
		if err := json.Unmarshal(m.Extract, &round); err != nil {
			t.Fatalf("m.Extract is not decodable JSON (%q): %v", m.Extract, err)
		}
		if len(round) != 2 {
			t.Fatalf("round-tripped %d entries, want 2", len(round))
		}
		if round[0].Dest != "/usr/one" || round[1].Dest != "/usr/two" {
			t.Fatalf("round-tripped entries = %+v, want the authored values in order", round)
		}
	})

	t.Run("no authored extractions leaves the body empty", func(t *testing.T) {
		m := &spec.CandyModel{}
		populateFromYAML(m, &spec.CandyView{}, &spec.CandyYAML{Name: "demo"})
		if len(m.Extract) != 0 {
			t.Fatalf("m.Extract = %q for a candy with no extractions, want empty", m.Extract)
		}
	})
}
