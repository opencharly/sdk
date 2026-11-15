package loaderkit

import (
	"encoding/json"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestPopulateFromYAMLWritesVolumesAndAliases is the producer test for the two fields that have NO
// adapter accessor — the adapter reads their CandyView twins — so this is the ONLY place their
// encoding is pinned. The body's coverage claim rests on it.
//
// The IR carries them as OPAQUE BYTES (DESIGN.md 3.1, I1/D-SCH-7): the producer must write exactly
// the JSON the authored lists serialise to, and must write NOTHING for an empty list, because "[]"
// is two non-empty bytes and a len(body) > 0 predicate would answer true for a candy declaring none.
func TestPopulateFromYAMLWritesVolumesAndAliases(t *testing.T) {
	t.Run("authored lists become their JSON bytes", func(t *testing.T) {
		m := &spec.CandyModel{}
		ly := &spec.CandyYAML{
			Name:   "demo",
			Volume: []spec.CandyVolume{{Name: "workspace", Path: "/workspace"}},
			Alias:  []spec.CandyAlias{{Name: "app", Command: "/opt/app"}},
		}
		populateFromYAML(m, &spec.CandyView{}, ly)

		if len(m.Volumes) == 0 {
			t.Fatal("m.Volumes is empty — the producer dropped the authored volumes")
		}
		var vols []spec.CandyVolume
		if err := json.Unmarshal(m.Volumes, &vols); err != nil {
			t.Fatalf("m.Volumes is not decodable JSON (%q): %v", m.Volumes, err)
		}
		if len(vols) != 1 || vols[0].Name != "workspace" || vols[0].Path != "/workspace" {
			t.Fatalf("round-tripped volumes = %+v, want the authored entry", vols)
		}

		if len(m.Aliases) == 0 {
			t.Fatal("m.Aliases is empty — the producer dropped the authored aliases")
		}
		var als []spec.CandyAlias
		if err := json.Unmarshal(m.Aliases, &als); err != nil {
			t.Fatalf("m.Aliases is not decodable JSON (%q): %v", m.Aliases, err)
		}
		if len(als) != 1 || als[0].Name != "app" || als[0].Command != "/opt/app" {
			t.Fatalf("round-tripped aliases = %+v, want the authored entry", als)
		}
	})

	t.Run("no authored lists leave the bodies empty", func(t *testing.T) {
		m := &spec.CandyModel{}
		populateFromYAML(m, &spec.CandyView{}, &spec.CandyYAML{Name: "demo"})
		if len(m.Volumes) != 0 || len(m.Aliases) != 0 {
			t.Fatalf("m.Volumes=%q m.Aliases=%q for a candy with neither, want empty bodies", m.Volumes, m.Aliases)
		}
	})

	// The empty-non-nil case: the guard rule these two fields share with every other slice.
	t.Run("EMPTY non-nil lists are also no bodies", func(t *testing.T) {
		m := &spec.CandyModel{}
		populateFromYAML(m, &spec.CandyView{}, &spec.CandyYAML{
			Name:   "demo",
			Volume: []spec.CandyVolume{},
			Alias:  []spec.CandyAlias{},
		})
		if len(m.Volumes) != 0 || len(m.Aliases) != 0 {
			t.Fatalf("m.Volumes=%q m.Aliases=%q for EMPTY non-nil lists, want empty bodies", m.Volumes, m.Aliases)
		}
	})
}
