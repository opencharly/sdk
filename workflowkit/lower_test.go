package workflowkit

import (
	"flag"
	"os"
	"path/filepath"
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
