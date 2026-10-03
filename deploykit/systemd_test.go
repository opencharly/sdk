package deploykit

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The unit and timer golden tests both live in this file and share this `-update` switch:
// the emitted text is what the operator reads and what systemd parses, so a change to it
// must be a reviewable diff, never a silent drift.
var updateGoldens = false

func TestMain(m *testing.M) {
	flag.BoolVar(&updateGoldens, "update", false, "regenerate testdata/*.golden")
	flag.Parse()
	os.Exit(m.Run())
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run `go test ./deploykit -update` to regenerate): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("%s mismatch:\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

func TestGenerateSystemdUnitGolden(t *testing.T) {
	got := GenerateSystemdUnit(SystemdUnitConfig{
		Name:        "web",
		Description: "charly deployment web",
		StartArgv:   []string{"/usr/bin/nerdctl", "run", "--rm", "--name", "web", "--userns=keep-id:uid=1000,gid=1000", "img"},
		StopArgv:    []string{"/usr/bin/nerdctl", "stop", "web"},
		ExecStartPre: [][]string{
			{"/usr/bin/nerdctl", "rm", "-f", "web"},
		},
		Environment:      map[string]string{"B": "two words", "A": "100%", "C": "plain"},
		WorkingDirectory: "/home/u",
	})
	checkGolden(t, "systemd-unit.service.golden", got)
}

func TestGenerateSystemdUnitOneshotGolden(t *testing.T) {
	got := GenerateSystemdUnit(SystemdUnitConfig{
		Name:        "nightly",
		Description: "charly workflow nightly",
		StartArgv:   []string{"/usr/local/bin/charly", "-C", "/home/u/p/.opencharly/pipelines/nightly", "task", "_pipeline-nightly-run", "--output"},
		StopArgv:    []string{"/usr/bin/true"},
		Type:        "oneshot",
		OmitStop:    true,
	})
	checkGolden(t, "systemd-oneshot.service.golden", got)
}

func TestGenerateSystemdUnitEmptyStart(t *testing.T) {
	if got := GenerateSystemdUnit(SystemdUnitConfig{Name: "x"}); got != "" {
		t.Errorf("a unit with no StartArgv must render empty, got:\n%s", got)
	}
}

func TestGenerateSystemdTimerGolden(t *testing.T) {
	got := GenerateSystemdTimer(TimerConfig{
		Name:               "nightly",
		Description:        "charly workflow nightly",
		OnCalendar:         "*-*-* 03:00:00",
		Persistent:         true,
		RandomizedDelaySec: "15m",
		Service: SystemdUnitConfig{
			StartArgv: []string{"/usr/local/bin/charly", "-C", "/home/u/p/.opencharly/pipelines/nightly", "task", "_pipeline-nightly-run", "--output"},
		},
	})
	checkGolden(t, "systemd-timer.timer.golden", got)
}

func TestGenerateTimerServiceIsOneshotAndStopless(t *testing.T) {
	got := GenerateTimerService(TimerConfig{
		Name:       "nightly",
		OnCalendar: "*-*-* 03:00:00",
		Service: SystemdUnitConfig{
			StartArgv: []string{"/usr/local/bin/charly", "task", "run"},
			StopArgv:  []string{"/usr/bin/true"},
		},
	})
	if !strings.Contains(got, "Type=oneshot\n") {
		t.Errorf("timer service must be Type=oneshot:\n%s", got)
	}
	if strings.Contains(got, "ExecStop=") {
		t.Errorf("timer service must carry no ExecStop:\n%s", got)
	}
	if !strings.Contains(got, "Restart=no\n") || strings.Contains(got, "RestartSec=") {
		t.Errorf("timer service must default to Restart=no without RestartSec:\n%s", got)
	}
	checkGolden(t, "systemd-timer.service.golden", got)
}

func TestGenerateSystemdTimerEmptyOnCalendar(t *testing.T) {
	if got := GenerateSystemdTimer(TimerConfig{Name: "x"}); got != "" {
		t.Errorf("a timer with no OnCalendar must render empty, got:\n%s", got)
	}
}

// TestResugarNested is the INVERSE-direction check of the loader's nested-plan desugar:
// a body carrying an internal plugin/plugin_input pair at ANY depth (a nested task plan,
// a parallel-branch plan) must rewrite back to the authored `<word>: <input>` sugar, so a
// written file round-trips through the parse instead of tripping its authored-envelope ban.
func TestResugarNested(t *testing.T) {
	const src = `
task:
  description: nested
  plan:
    - run: a plugin verb
      plugin: task
      plugin_input:
        task: hello
parallel:
  branches:
    - id: b1
      plan:
        - check: a check
          plugin: http
          plugin_input:
            url: "http://x"
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	body := doc.Content[0]
	ResugarNested(body, nil)
	out, err := yaml.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "plugin_input") || strings.Contains(got, "plugin:") {
		t.Errorf("ResugarNested left the internal pair in place:\n%s", got)
	}
	if !strings.Contains(got, "description: nested") {
		t.Errorf("ResugarNested dropped a sibling key:\n%s", got)
	}

	// Check the STRUCTURE of every plan found at any depth, so the assertion does not
	// couple to yaml indentation.
	plans := findPlans(body)
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans in the body, found %d:\n%s", len(plans), got)
	}
	byWord := map[string]*yaml.Node{}
	for _, pl := range plans {
		for _, st := range pl.Content {
			for i := 0; i+1 < len(st.Content); i += 2 {
				byWord[st.Content[i].Value] = st.Content[i+1]
			}
		}
	}
	if len(byWord["task"].Content) == 0 || childValue(byWord["task"], "task") == nil {
		t.Errorf("nested task plan not resugared to the authored `task: {task: hello}`:\n%s", got)
	}
	if childValue(byWord["http"], "url") == nil {
		t.Errorf("parallel-branch plan not resugared to the authored `http: {url: …}`:\n%s", got)
	}
}

// findPlans returns every sequence node that sits under a `plan:` key in a subtree.
func findPlans(n *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		switch n.Kind {
		case yaml.SequenceNode:
			for _, c := range n.Content {
				walk(c)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "plan" && n.Content[i+1].Kind == yaml.SequenceNode {
					out = append(out, n.Content[i+1])
					continue
				}
				walk(n.Content[i+1])
			}
		}
	}
	walk(n)
	return out
}

// childValue returns the value node for key in a mapping node, or nil.
func childValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
