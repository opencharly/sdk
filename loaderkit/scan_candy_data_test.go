package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestPopulateFromYAMLWritesDataAsJSON is the data producer test, the third on this pattern — and it
// exercises the REAL populateFromYAML, because a fixture that marshals locally proves the fixture.
//
// The IR carries the data list as OPAQUE BYTES (DESIGN.md 3.1, I1/D-SCH-7): the producer must write
// exactly the JSON the authored #CandyData list serialises to, and MUST NOT write anything at all
// for an empty list — "[]" is two non-empty bytes, which would make HasData() answer true for a
// candy that declares no data seeding.
func TestPopulateFromYAMLWritesDataAsJSON(t *testing.T) {
	t.Run("authored data becomes its JSON bytes", func(t *testing.T) {
		m := &spec.CandyModel{}
		ly := &spec.CandyYAML{
			Name: "demo",
			Data: []spec.CandyData{
				{Volume: "workspace", Src: "/seed", Dest: "/data/one"},
			},
		}
		populateFromYAML(m, &spec.CandyView{}, ly)

		if len(m.Data) == 0 {
			t.Fatal("m.Data is empty — the producer dropped the authored data list")
		}
		var round []spec.CandyData
		if err := json.Unmarshal(m.Data, &round); err != nil {
			t.Fatalf("m.Data is not decodable JSON (%q): %v", m.Data, err)
		}
		if len(round) != 1 || round[0].Dest != "/data/one" {
			t.Fatalf("round-tripped = %+v, want the authored entry", round)
		}
	})

	t.Run("no authored data leaves the body empty", func(t *testing.T) {
		m := &spec.CandyModel{}
		populateFromYAML(m, &spec.CandyView{}, &spec.CandyYAML{Name: "demo"})
		if len(m.Data) != 0 {
			t.Fatalf("m.Data = %q for a candy with no data, want empty", m.Data)
		}
	})

	// An EMPTY non-nil slice is the case that slips through a `!= nil` guard: marshalling it writes
	// "[]", two non-empty bytes, and HasData() would answer true.
	t.Run("an empty non-nil list is also no data", func(t *testing.T) {
		m := &spec.CandyModel{}
		populateFromYAML(m, &spec.CandyView{}, &spec.CandyYAML{Name: "demo", Data: []spec.CandyData{}})
		if len(m.Data) != 0 {
			t.Fatalf("m.Data = %q for an EMPTY non-nil data list, want empty — otherwise HasData() answers true", m.Data)
		}
	})
}
