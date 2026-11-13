// parse_candy_manifest_fields_test.go — the candy BODY's load-time field gate is the CLOSED `#Candy`
// schema, not a hand-maintained list.
//
// The gate (`rejectUnknownCandyTopLevelKeys`) exists because the load-time CUE decode is LENIENT
// (DecodeEntityViaCUE does BuildFile+Decode with no Unify against the schema), so a plural/singular
// typo or a retired field would be silently dropped at load and only caught by `charly box validate`.
// Its admitted set used to be a hand-written copy of the same keys, and nothing tied that copy to
// `#Candy` — so it drifted in BOTH directions: it still admitted four fields the schema had retired
// (`version`, `from`, `libvirt`, `localpkg`) and it rejected five fields the schema declares
// (`external_builder`, `skills`, `packaging`, `agent_provide`, `terminal_profile`), making a legacy
// manifest that used any of those five fail to load under a bogus typo message.
//
// These tests pin both directions against the SCHEMA, so a future field lands (or retires) in one
// place: the CUE contract.
package loaderkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// retiredCandyBodyFields are fields `#Candy` no longer models and whose removal carries a migration
// or a routing answer, so rejectLegacyCandyKeys names each one instead of falling through to the
// generic unknown-key message.
var retiredCandyBodyFields = map[string]string{
	"version":  "the version stamp is retired",
	"libvirt":  "raw-XML snippets are retired",
	"localpkg": "declare the native-package metadata under `packaging:`",
	"from":     "`from:` is a #Box (image) field",
}

// legacyCandyThreadeds are the two Threaded snapshots the gate must hold under: EMPTY (a resolver
// context before the provider registry exists — the sdk#323 fallback shape) and a REGISTRY snapshot
// that recognizes the candy family, which is what a host-side load passes. Both must reach the gate;
// if kind classification diverted the legacy form past it, the whole test set would pass while the
// field gate never ran.
func legacyCandyThreadeds() []legacyThreaded {
	registry := spec.Threaded{
		Kinds:            map[string]bool{"candy": true, "skill": true, "hook": true, "marketplace": true},
		DeploySubstrates: map[string]bool{},
		StructuralKinds:  map[string]bool{"candy": true},
	}
	return []legacyThreaded{
		{Name: "empty-threaded"},
		{Name: "registry-threaded", T: registry},
	}
}

// legacyThreaded is one Threaded snapshot to run a case under, with a name for the subtest label.
type legacyThreaded struct {
	Name string
	T    spec.Threaded
}

// Label namespaces a subtest under this Threaded snapshot.
func (l legacyThreaded) Label(sub string) string { return l.Name + "/" + sub }

// parseLegacyCandyManifest writes body as a kind-keyed candy manifest (`candy:` as the SOLE
// top-level key — the form the load-time field gate runs on) and parses it.
func parseLegacyCandyManifest(t *testing.T, th spec.Threaded, body string) (*spec.CandyYAML, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "charly.yml")
	if err := os.WriteFile(path, []byte("candy:\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return ParseCandyManifest(path, th, spec.CandyVocab{})
}

// TestCandyBodyFieldsAreTheClosedSchema is the drift pin: the gate's admitted set EQUALS `#Candy`'s
// own top-level field set, with no entry of its own and no field of the schema missing. It fails
// both ways — an admitted name the schema does not model, and a schema field the gate would refuse.
func TestCandyBodyFieldsAreTheClosedSchema(t *testing.T) {
	schemaFields := cueDefFields("#Candy")
	if len(schemaFields) == 0 {
		t.Fatal("cueDefFields(#Candy) is empty — the schema handle did not resolve")
	}
	admitted := candyBodyFields()
	for f := range schemaFields {
		if !admitted[f] {
			t.Errorf("`%s:` is declared by #Candy but the load-time gate refuses it", f)
		}
	}
	for f := range admitted {
		if !schemaFields[f] {
			t.Errorf("`%s:` is admitted by the load-time gate but #Candy does not model it", f)
		}
	}
	for f := range retiredCandyBodyFields {
		if admitted[f] {
			t.Errorf("retired field `%s:` is still admitted", f)
		}
	}
}

// TestParseCandyManifest_RejectsRetiredCandyBodyFields proves each retired field is refused AT LOAD
// with the diagnosis that names its retirement — not the generic plural/singular-typo message, which
// would send the author looking for a typo that does not exist.
func TestParseCandyManifest_RejectsRetiredCandyBodyFields(t *testing.T) {
	for _, th := range legacyCandyThreadeds() {
		for field, want := range retiredCandyBodyFields {
			t.Run(th.Label(field), func(t *testing.T) {
				_, err := parseLegacyCandyManifest(t, th.T, fmt.Sprintf("    %s: x\n    description: retired field fixture\n", field))
				if err == nil {
					t.Fatalf("`%s:` was admitted; the closed #Candy schema does not model it", field)
				}
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error does not name the retirement: got %v, want it to contain %q", err, want)
				}
				if strings.Contains(err.Error(), "plural/singular typo") {
					t.Fatalf("a retirement is not a typo, but the generic typo message was returned: %v", err)
				}
			})
		}
	}
}

// TestParseCandyManifest_AdmitsEverySchemaField is the other direction: a field the CLOSED schema
// declares must load. The four sampled here (`skills`, `packaging`, `external_builder`,
// `agent_provide`) were all refused by the hand-maintained list.
func TestParseCandyManifest_AdmitsEverySchemaField(t *testing.T) {
	cases := map[string]string{
		"skills":           "    skills: [custom/skills]\n",
		"packaging":        "    packaging: {}\n",
		"external_builder": "    external_builder: mise\n",
		"agent_provide":    "    agent_provide: []\n",
	}
	for _, th := range legacyCandyThreadeds() {
		for field, body := range cases {
			t.Run(th.Label(field), func(t *testing.T) {
				c, err := parseLegacyCandyManifest(t, th.T, body+"    description: schema field fixture\n")
				if err != nil {
					t.Fatalf("`%s:` is declared by #Candy but the load refused it: %v", field, err)
				}
				if c.Description != "schema field fixture" {
					t.Fatalf("body decoded as %+v, want the fixture description", c)
				}
			})
		}
	}
}

// TestParseCandyManifest_UnknownKeyStaysATypoMessage keeps the load-time typo detection that the
// schema-derived set replaced the hand copy FOR: an unknown key is still refused, and the message
// still names the singular remedy.
func TestParseCandyManifest_UnknownKeyStaysATypoMessage(t *testing.T) {
	for _, th := range legacyCandyThreadeds() {
		_, err := parseLegacyCandyManifest(t, th.T, "    description: typo fixture\n    tasks: [a]\n")
		if err == nil {
			t.Fatalf("%s: an unknown top-level key was admitted", th.Name)
		}
		if !strings.Contains(err.Error(), "plural/singular typo") || !strings.Contains(err.Error(), "tasks") {
			t.Fatalf("%s: unknown-key error lost its typo diagnosis: %v", th.Name, err)
		}
	}
}
