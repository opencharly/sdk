package loaderkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// docFrom parses a YAML document string into its root node.
func docFrom(t *testing.T, s string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return &n
}

// podThreaded recognizes `pod` as a kind that nests members and `http` as a sugar verb whose
// scalar-shorthand primary is `http` — enough to exercise classify + desugar + member nesting.
var podThreaded = spec.Threaded{
	Kinds:            map[string]bool{"pod": true},
	DeploySubstrates: map[string]bool{},
	StructuralKinds:  map[string]bool{"pod": true},
	Primaries:        map[string]string{"http": "http"},
}

func TestParseDoc_Entity(t *testing.T) {
	dirs, pp, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("directives = %v, want none", dirs)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(pp.Nodes))
	}
	n := pp.Nodes[0]
	if n.Name != "web" || n.Disc != "pod" {
		t.Errorf("node = %q/%q, want web/pod", n.Name, n.Disc)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(n.Body), &body); err != nil {
		t.Fatalf("body json: %v", err)
	}
	if body["from"] != "img" {
		t.Errorf("body.from = %v, want img", body["from"])
	}
}

func TestParseDoc_DirectivesSkipped(t *testing.T) {
	dirs, pp, err := ParseDoc(docFrom(t, `
version: 2026.192.0000
web:
  pod:
    from: img
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	// version is a doc directive — skipped from the entity nodes.
	if len(pp.Nodes) != 1 || pp.Nodes[0].Name != "web" {
		t.Fatalf("nodes = %+v, want just web", pp.Nodes)
	}
	_ = dirs
}

func TestParseDoc_DesugarsSugarVerb(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    plan:
      - check: it serves
        http: http://localhost/
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	body := string(pp.Nodes[0].Body)
	// The `http: <url>` sugar must have desugared to the plugin/plugin_input envelope.
	if !strings.Contains(body, `"plugin":"http"`) || !strings.Contains(body, `"plugin_input"`) {
		t.Fatalf("sugar not desugared in body: %s", body)
	}
}

func TestParseDoc_MemberChild(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
  db:
    pod:
      from: dbimg
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if len(pp.Nodes) != 1 {
		t.Fatalf("top nodes = %d, want 1", len(pp.Nodes))
	}
	if len(pp.Nodes[0].Children) != 1 || pp.Nodes[0].Children[0].Name != "db" {
		t.Fatalf("children = %+v, want one 'db'", pp.Nodes[0].Children)
	}
}

func TestParseDoc_NoDiscriminatorErrors(t *testing.T) {
	if _, _, err := ParseDoc(docFrom(t, `
web:
  from: img
`), podThreaded); err == nil {
		t.Fatal("want an error for a node with no kind discriminator")
	}
}

func TestParseDoc_DuplicateNameErrors(t *testing.T) {
	if _, _, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: a
web:
  pod:
    from: b
`), podThreaded); err == nil {
		t.Fatal("want a duplicate-name error")
	}
}
