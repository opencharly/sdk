package loaderkit

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/opencharly/spec/spec"
)

// The REAL authoring shape — intent keyword, id, context, stdout matcher shorthand AND a
// plugin-verb sugar key — must desugar to the wire form without misfiring on the step's own
// fields. This is the exact shape a user-authored --steps-file carries.
func TestDesugarStepsRealStepShape(t *testing.T) {
	doc := "- check: the emulator is attached\n" +
		"  id: g8-devices-online\n" +
		"  adb:\n" +
		"    method: devices\n" +
		"  context:\n" +
		"    - runtime\n" +
		"  stdout:\n" +
		"    - contains: emulator-5554\n"
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
		t.Fatal(err)
	}
	if err := DesugarSteps("steps.yml", &n, spec.Threaded{}); err != nil {
		t.Fatalf("real step shape must desugar cleanly: %v", err)
	}
	out, err := yaml.Marshal(&n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	t.Logf("desugared: %s", s)
	if !strings.Contains(s, "plugin: adb") || !strings.Contains(s, "plugin_input:") {
		t.Fatalf("verb sugar not rewritten: %s", s)
	}
	if !strings.Contains(s, "g8-devices-online") || !strings.Contains(s, "runtime") {
		t.Fatalf("id/context lost: %s", s)
	}
	// The matcher shorthand is preserved (it is decoded on the consumer's JSON path).
	if !strings.Contains(s, "contains") {
		t.Fatalf("matcher shorthand lost: %s", s)
	}
}

func TestDesugarStepsWireFormPassesThrough(t *testing.T) {
	doc := "- check: wire form\n" +
		"  id: w1\n" +
		"  plugin: adb\n" +
		"  plugin_input:\n" +
		"    method: devices\n"
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
		t.Fatal(err)
	}
	if err := DesugarSteps("steps.yml", &n, spec.Threaded{}); err != nil {
		t.Fatalf("wire form must pass through: %v", err)
	}
	out, _ := yaml.Marshal(&n)
	if !strings.Contains(string(out), "plugin: adb") {
		t.Fatalf("wire form changed: %s", out)
	}
}

func TestDesugarStepsRejectsNonSequenceAndNoIntent(t *testing.T) {
	var m yaml.Node
	if err := yaml.Unmarshal([]byte("check: x\n"), &m); err != nil {
		t.Fatal(err)
	}
	if err := DesugarSteps("steps.yml", &m, spec.Threaded{}); err == nil {
		t.Fatal("a mapping document must error: a steps sequence must be a YAML list")
	}
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("- id: no-intent\n  adb:\n    method: devices\n"), &n); err != nil {
		t.Fatal(err)
	}
	if err := DesugarSteps("steps.yml", &n, spec.Threaded{}); err == nil {
		t.Fatal("a step without an intent keyword must error")
	}
}
