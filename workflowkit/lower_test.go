package workflowkit

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// lower_test.go — the Lower golden tests. They assert the generated TEXT only, so the
// gate needs no live engine: a lowering regression (a dropped ref, a mis-resugared plan,
// a lost `--output`) shows up as a diff.

// updateGoldens regenerates testdata/*.golden when set (`go test -run TestLower -update`).
var updateGoldens = false

func TestMain(m *testing.M) {
	flag.BoolVar(&updateGoldens, "update", false, "regenerate testdata/*.golden")
	flag.Parse()
	os.Exit(m.Run())
}

// e2ePipeline mirrors the workflow the RDD executed live end-to-end: a run step producing
// JSON, a plan step consuming it, an approval gate, and a when-guarded final step.
func e2ePipeline() *spec.Pipeline {
	return &spec.Pipeline{
		Description: "end-to-end lowering fixture",
		Args: map[string]spec.TaskParamSpec{
			"who": {Description: "who to greet", Default: "world"},
		},
		Steps: []spec.PipelineStep{
			{Id: "fetch", Run: `printf '{"name":"ok","n":3}'`},
			{Id: "probe", Plan: []spec.Step{
				{Run: "the ref reaches the charly step shell-safely", Op: spec.Op{Command: `printf "NAME=%s\n" "$fetch.json.name"`}},
				{Run: "a plugin verb dispatches through the core registry", Op: spec.Op{Plugin: "task", PluginInput: map[string]any{"task": "hello"}}},
				{Run: "a passed-through env var expands", Op: spec.Op{Command: `echo "N=$fetch.json.n"`}},
			}},
			{Id: "confirm", Approval: "Proceed with ${who}?"},
			{Id: "final", When: "$confirm.approved", Run: `echo '{"done":true}'`},
		},
	}
}

func TestLowerGolden(t *testing.T) {
	p := e2ePipeline()
	lobster, charly, err := Lower(p, "/proj", "/proj/.opencharly/pipelines/e2e", "/usr/local/bin/charly")
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	checkGolden(t, "e2e.workflow.lobster.golden", lobster)
	checkGolden(t, "e2e.charly.yml.golden", charly)
}

// TestLowerRefsBraceForm pins the FORM of the ref the lowering writes into a generated
// plan. The expander that runs that plan (kit.ExpandOpVars, whose grammar is
// kit.TestVarRefPattern) is BRACE-ONLY, so a ref consumed as a non-shell verb input must
// be emitted as ${REF_n}: the bare $REF_n form is passed through as literal text.
func TestLowerRefsBraceForm(t *testing.T) {
	p := &spec.Pipeline{
		Description: "ref-form fixture",
		Steps: []spec.PipelineStep{
			{Id: "fetch", Run: `printf '{"name":"ok"}'`},
			{Id: "probe", Plan: []spec.Step{
				// a NON-SHELL verb input carrying the ref (a plugin verb's opaque field).
				{Run: "the ref reaches a plugin verb input", Op: spec.Op{Plugin: "task", PluginInput: map[string]any{"task": "$fetch.json.name"}}},
			}},
		},
	}
	_, charly, err := Lower(p, "/proj", "/proj/.opencharly/pipelines/ref", "/usr/local/bin/charly")
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	plan := string(charly)
	braceRef := "${REF_1}"
	if !strings.Contains(plan, braceRef) {
		t.Errorf("generated plan does not carry the brace form %s:\n%s", braceRef, plan)
	}
	if strings.Contains(plan, "$REF_1") {
		t.Errorf("generated plan still carries the bare form $REF_1 (never expanded):\n%s", plan)
	}
	// The plan's ref must be matched by kit.ExpandOpVars' brace-only grammar.
	braceOnly := regexp.MustCompile(`\$\{[A-Z_][A-Z0-9_]*\}`)
	if !braceOnly.MatchString(braceRef) {
		t.Fatalf("%s is not matched by the plan expander's brace-only pattern", braceRef)
	}
}

// redoPipeline is the migrated-stage shape: ONE lobster step whose plan is ONE
// `{<verb>: {…, redo: {…}}}` pair (what plugin-migrate's reshapePipelineVerbStage emits).
// The redo spec lives in the verb body and must be COPIED onto the lobster step.
func redoPipeline() *spec.Pipeline {
	return &spec.Pipeline{
		Description: "redo-carry fixture",
		Steps: []spec.PipelineStep{
			{Id: "oracle", Plan: []spec.Step{
				{Op: spec.Op{Plugin: "task", PluginInput: map[string]any{
					"task": "oracle",
					"redo": map[string]any{
						"max":            4,
						"escalate_after": 5,
						"triggers":       map[string]any{"redo-plan": "oracle"},
					},
				}}},
			}},
			{Id: "plain", Plan: []spec.Step{
				{Op: spec.Op{Plugin: "task", PluginInput: map[string]any{"task": "plain"}}},
			}},
		},
	}
}

// TestLowerRedoCarry proves the redo spec is COPIED, not moved: the engine reads it off
// the lobster STEP (which would otherwise be `{id, run}` only, since `plan:` is deleted),
// while the VERB still reads `redo.on_fail` from its own input.
func TestLowerRedoCarry(t *testing.T) {
	lobster, charly, err := Lower(redoPipeline(), "/proj", "/proj/.opencharly/pipelines/redo", "/usr/local/bin/charly")
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	checkGolden(t, "redo.workflow.lobster.golden", lobster)
	checkGolden(t, "redo.charly.yml.golden", charly)

	// BOTH halves must carry it — either alone would pass under a wrong implementation.
	if !strings.Contains(string(lobster), "redo:") {
		t.Errorf("lobster step does not carry the redo spec (engine cannot rewind):\n%s", lobster)
	}
	if !strings.Contains(string(charly), "redo:") {
		t.Errorf("generated charly verb input lost the redo spec (move, not copy):\n%s", charly)
	}
	// The step with no plan redo must gain NO `redo:` key at all.
	if i := strings.Index(string(lobster), "id: plain"); i >= 0 && strings.Contains(string(lobster)[i:], "redo:") {
		t.Errorf("a plan step with no redo gained a `redo:` key:\n%s", lobster)
	}
}

// TestLowerNoRedoEmitsNoKey pins the defensive half: no redo child → no `redo:` key
// anywhere (never an empty `redo: {}`), so a plain plan lowers byte-identically to before.
func TestLowerNoRedoEmitsNoKey(t *testing.T) {
	lobster, charly, err := Lower(e2ePipeline(), "/proj", "/proj/.opencharly/pipelines/e2e", "/usr/local/bin/charly")
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	if strings.Contains(string(lobster), "redo") {
		t.Errorf("lowered a step with no redo but emitted a `redo` key:\n%s", lobster)
	}
	if strings.Contains(string(charly), "redo") {
		t.Errorf("generated charly.yml with no redo gained a `redo` key:\n%s", charly)
	}
}

func TestLowerCharlyStep(t *testing.T) {
	p := &spec.Pipeline{
		Description: "charly-arm fixture",
		Steps: []spec.PipelineStep{
			{Id: "build", Charly: []string{"box", "build", "my box"}},
			{Id: "after", When: "$build.exit_code == 0", Run: "true"},
		},
	}
	lobster, charly, err := Lower(p, "/proj", "/proj/.opencharly/pipelines/ci", "/usr/local/bin/charly")
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	checkGolden(t, "charly-step.workflow.lobster.golden", lobster)
	checkGolden(t, "charly-step.charly.yml.golden", charly)
}

func TestLowerNestedPlan(t *testing.T) {
	p := &spec.Pipeline{
		Description: "nested plan fixture",
		Steps: []spec.PipelineStep{
			{Id: "setup", Run: "true"},
			{Id: "fan", Parallel: spec.PipelineParallel{
				Wait: "all",
				Branches: []spec.PipelineSubStep{
					{Id: "b1", Plan: []spec.Step{
						{Run: "branch one", Op: spec.Op{Plugin: "task", PluginInput: map[string]any{"task": "one"}}},
					}},
					{Id: "b2", Run: "echo $setup.exit_code"},
				},
			}},
		},
	}
	lobster, charly, err := Lower(p, "/proj", "/proj/.opencharly/pipelines/fan", "/usr/local/bin/charly")
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	checkGolden(t, "nested.workflow.lobster.golden", lobster)
	checkGolden(t, "nested.charly.yml.golden", charly)
}

func TestLowerRejectsInvalid(t *testing.T) {
	// a step with two exec arms never lowers.
	p := &spec.Pipeline{Steps: []spec.PipelineStep{{Id: "bad", Run: "true", Charly: []string{"status"}}}}
	if _, _, err := Lower(p, "/proj", "/proj/.opencharly/pipelines/x", "/bin/charly"); err == nil {
		t.Fatal("Lower accepted a step with two execution arms")
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run `go test ./workflowkit -update` to regenerate): %v", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("%s mismatch:\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
