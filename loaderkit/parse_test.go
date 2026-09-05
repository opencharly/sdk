package loaderkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
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

// bodyMap unmarshals a parsed node body JSON into a map for assertions.
func bodyMap(t *testing.T, body json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body json: %v", err)
	}
	return m
}

func TestParseDoc_DesugarsInstrumentVerb(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        phase: [live]
        spice: {method: session, fps: 5}
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	body := bodyMap(t, pp.Nodes[0].Body)
	inst, ok := body["instrument"].([]any)
	if !ok || len(inst) != 1 {
		t.Fatalf("instrument = %v, want exactly one entry", body["instrument"])
	}
	entry, ok := inst[0].(map[string]any)
	if !ok {
		t.Fatalf("instrument[0] not a map: %v", inst[0])
	}
	if entry["id"] != "screen" {
		t.Errorf("entry.id = %v, want screen", entry["id"])
	}
	if entry["plugin"] != "spice" {
		t.Errorf("entry.plugin = %v, want spice", entry["plugin"])
	}
	pi, ok := entry["plugin_input"].(map[string]any)
	if !ok {
		t.Fatalf("entry.plugin_input not a map: %v", entry["plugin_input"])
	}
	if pi["method"] != "session" || pi["fps"] != float64(5) {
		t.Errorf("entry.plugin_input = %v, want {method: session, fps: 5}", pi)
	}
}

func TestParseDoc_InstrumentVerbMatchesStepVerbatim(t *testing.T) {
	// The SAME verb+input must desugar byte-identically whether it sits on a plan step
	// or on an instrument entry's verb position.
	_, stepPP, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    plan:
      - check: probe the display
        spice: {method: session, fps: 5}
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc (step): %v", err)
	}
	_, instPP, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        phase: [live]
        spice: {method: session, fps: 5}
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc (instrument): %v", err)
	}
	stepBody := bodyMap(t, stepPP.Nodes[0].Body)
	instBody := bodyMap(t, instPP.Nodes[0].Body)
	stepEntry, ok := stepBody["plan"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("step entry not a map: %v", stepBody["plan"])
	}
	instEntry, ok := instBody["instrument"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("instrument entry not a map: %v", instBody["instrument"])
	}
	stepVerb, _ := json.Marshal(stepEntry["plugin"])
	instVerb, _ := json.Marshal(instEntry["plugin"])
	stepInput, _ := json.Marshal(stepEntry["plugin_input"])
	instInput, _ := json.Marshal(instEntry["plugin_input"])
	if string(stepVerb) != string(instVerb) || string(stepInput) != string(instInput) {
		t.Fatalf("desugared pair differs — step %s %s vs instrument %s %s", stepVerb, stepInput, instVerb, instInput)
	}
	if instEntry["plugin"] != "spice" {
		t.Errorf("entry.plugin = %v, want spice", instEntry["plugin"])
	}
	if string(stepVerb) != "\"spice\"" {
		t.Errorf("step.plugin = %s, want \"spice\"", stepVerb)
	}
}

func TestParseDoc_DesugarsInstrumentScalarShorthand(t *testing.T) {
	// Scalar shorthand in a capture position wraps the verb's declared primary — the
	// same rule as a step.
	_, pp, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: probe
        phase: [live]
        http: http://localhost/
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	body := bodyMap(t, pp.Nodes[0].Body)
	entry := body["instrument"].([]any)[0].(map[string]any)
	if entry["plugin"] != "http" {
		t.Errorf("entry.plugin = %v, want http", entry["plugin"])
	}
	pi, ok := entry["plugin_input"].(map[string]any)
	if !ok || pi["http"] != "http://localhost/" {
		t.Errorf("entry.plugin_input = %v, want {http: http://localhost/}", entry["plugin_input"])
	}
}

func TestParseDoc_DesugarsPipelineWord(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        phase: [live]
        spice: {method: session, fps: 5}
        pipeline:
          - transcode: {to: mp4}
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	body := bodyMap(t, pp.Nodes[0].Body)
	entry := body["instrument"].([]any)[0].(map[string]any)
	pl, ok := entry["pipeline"].([]any)
	if !ok || len(pl) != 1 {
		t.Fatalf("entry.pipeline = %v, want one word", entry["pipeline"])
	}
	word, ok := pl[0].(map[string]any)
	if !ok {
		t.Fatalf("pipeline[0] not a map: %v", pl[0])
	}
	if word["plugin"] != "transcode" {
		t.Errorf("pipeline[0].plugin = %v, want transcode", word["plugin"])
	}
	pi, ok := word["plugin_input"].(map[string]any)
	if !ok || pi["to"] != "mp4" {
		t.Errorf("pipeline[0].plugin_input = %v, want {to: mp4}", word["plugin_input"])
	}
	// The capture verb itself must still be desugared alongside.
	if entry["plugin"] != "spice" {
		t.Errorf("entry.plugin = %v, want spice", entry["plugin"])
	}
}

func TestParseDoc_DesugarsInstrumentOnMemberChild(t *testing.T) {
	_, pp, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
  db:
    pod:
      from: dbimg
      instrument:
        - id: term
          phase: [live]
          record: {what: terminal}
`), podThreaded)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	childBody := bodyMap(t, pp.Nodes[0].Children[0].Body)
	entry := childBody["instrument"].([]any)[0].(map[string]any)
	if entry["plugin"] != "record" {
		t.Errorf("child entry.plugin = %v, want record", entry["plugin"])
	}
}

func TestParseDoc_InstrumentInternalPairErrors(t *testing.T) {
	_, _, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        plugin: spice
        plugin_input: {}
`), podThreaded)
	if err == nil || !strings.Contains(err.Error(), "plugin envelope is internal-only") {
		t.Fatalf("want an internal-envelope load error, got %v", err)
	}
}

func TestParseDoc_PipelineInternalPairErrors(t *testing.T) {
	_, _, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        phase: [live]
        spice: {method: session}
        pipeline:
          - plugin_input: {to: mp4}
`), podThreaded)
	if err == nil || !strings.Contains(err.Error(), "plugin envelope is internal-only") {
		t.Fatalf("want an internal-envelope load error, got %v", err)
	}
}

func TestParseDoc_InstrumentMultipleVerbKeysErrors(t *testing.T) {
	_, _, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        spice: {method: session}
        record: {what: terminal}
`), podThreaded)
	if err == nil || !strings.Contains(err.Error(), "at most ONE verb-position sugar key") {
		t.Fatalf("want a multiple-verb-key error, got %v", err)
	}
}

func TestParseDoc_PipelineWordMultipleKeysErrors(t *testing.T) {
	_, _, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        phase: [live]
        spice: {method: session}
        pipeline:
          - transcode: {to: mp4}
            gif: {}
`), podThreaded)
	if err == nil || !strings.Contains(err.Error(), "exactly ONE verb key") {
		t.Fatalf("want a one-verb-key error, got %v", err)
	}
}

func TestParseDoc_InstrumentScalarWithoutPrimaryErrors(t *testing.T) {
	_, _, err := ParseDoc(docFrom(t, `
web:
  pod:
    from: img
    instrument:
      - id: screen
        phase: [live]
        transcode: mp4
`), podThreaded)
	if err == nil || !strings.Contains(err.Error(), "takes a MAP input") {
		t.Fatalf("want a MAP-input error, got %v", err)
	}
}
