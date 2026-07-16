package kit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/sdk/spec"
)

// grader_test.go — unit tests for the grader mechanism itself (P12a: relocated from
// charly/check_feature_grader_test.go). The RunPlan-dispatch-through-a-live-Runner
// tests stayed in charly (check_feature_grader_test.go) since they need the core
// Runner/LabelDescriptionSet machinery.

// --- parseVerdict --------------------------------------------------------

func TestParseVerdict_Plain(t *testing.T) {
	pass, ev, ok := parseVerdict("some reasoning here\n" + `{"verdict":"pass","evidence":"the port answered PONG"}`)
	if !ok || !pass || ev != "the port answered PONG" {
		t.Fatalf("got pass=%v ev=%q ok=%v", pass, ev, ok)
	}
}

func TestParseVerdict_Fail(t *testing.T) {
	pass, _, ok := parseVerdict(`{"verdict":"fail","evidence":"connection refused"}`)
	if !ok || pass {
		t.Fatalf("got pass=%v ok=%v, want fail", pass, ok)
	}
}

func TestParseVerdict_StreamJSON(t *testing.T) {
	out := `{"type":"system","subtype":"init"}` + "\n" +
		`{"type":"assistant","message":"checking..."}` + "\n" +
		`{"type":"result","subtype":"success","result":"I probed it. {\"verdict\":\"pass\",\"evidence\":\"ok\"}"}`
	pass, ev, ok := parseVerdict(out)
	if !ok || !pass || ev != "ok" {
		t.Fatalf("stream-json: pass=%v ev=%q ok=%v", pass, ev, ok)
	}
}

func TestParseVerdict_NoVerdict(t *testing.T) {
	if _, _, ok := parseVerdict("just prose, no json object at all"); ok {
		t.Fatal("expected no parseable verdict")
	}
}

func TestParseVerdict_LastWins(t *testing.T) {
	// An earlier illustrative example must not beat the final real verdict.
	out := `for example {"verdict":"fail"} but actually {"verdict":"pass","evidence":"done"}`
	pass, ev, ok := parseVerdict(out)
	if !ok || !pass || ev != "done" {
		t.Fatalf("last-wins: pass=%v ev=%q ok=%v", pass, ev, ok)
	}
}

// --- RunAgentOnce -----------------------------------------------------------

func TestRunAIOnce_CapturesStdout(t *testing.T) {
	ai := &spec.AgentExecSpec{Command: []string{"sh", "-c", `echo '{"verdict":"pass","evidence":"ok"}'`}}
	out, _, err := RunAgentOnce(context.Background(), ai, "ignored", 10*time.Second)
	if err != nil {
		t.Fatalf("RunAgentOnce: %v", err)
	}
	if pass, _, ok := parseVerdict(out); !ok || !pass {
		t.Fatalf("verdict not parsed from %q", out)
	}
}

func TestRunAIOnce_SubstitutesPrompt(t *testing.T) {
	ai := &spec.AgentExecSpec{Command: []string{"printf", "%s", "${PROMPT}"}}
	out, _, err := RunAgentOnce(context.Background(), ai, "HELLO-PROMPT-TOKEN", 10*time.Second)
	if err != nil {
		t.Fatalf("RunAgentOnce: %v", err)
	}
	if !strings.Contains(out, "HELLO-PROMPT-TOKEN") {
		t.Fatalf("${PROMPT} not substituted into argv: %q", out)
	}
}

func TestRunAIOnce_Timeout(t *testing.T) {
	ai := &spec.AgentExecSpec{Command: []string{"sleep", "10"}}
	_, _, err := RunAgentOnce(context.Background(), ai, "x", 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestRunAIOnce_NoCommand(t *testing.T) {
	if _, _, err := RunAgentOnce(context.Background(), &spec.AgentExecSpec{}, "x", time.Second); err == nil {
		t.Fatal("expected error for an ai entry with no command")
	}
}

// --- AgentGrader ---------------------------------------------------------

func TestAgentGrader_GradeFail(t *testing.T) {
	ai := &spec.AgentExecSpec{Command: []string{"sh", "-c", `echo '{"verdict":"fail","evidence":"port closed"}'`}}
	g := &AgentGrader{Agent: ai, Target: "check-pod"}
	res := g.Grade(context.Background(), GraderRequest{Keyword: "Then", Text: "the port answers"})
	if res.Status != StatusFail {
		t.Fatalf("want StatusFail, got %v", res.Status)
	}
	if !strings.Contains(res.Message, "port closed") {
		t.Fatalf("evidence not surfaced: %q", res.Message)
	}
}

func TestAgentGrader_UnparseableIsFail(t *testing.T) {
	ai := &spec.AgentExecSpec{Command: []string{"sh", "-c", `echo "I have no idea"`}}
	g := &AgentGrader{Agent: ai, Target: "check-pod"}
	res := g.Grade(context.Background(), GraderRequest{Keyword: "Then", Text: "x"})
	if res.Status != StatusFail {
		t.Fatalf("unparseable grader output must FAIL (never silent pass), got %v", res.Status)
	}
}

// --- buildGraderPrompt ---------------------------------------------------

// TestBuildGraderPrompt_PillarName is the check-coverage gate for the grader
// system prompt naming the ADE pillar ("Agent Driven Evaluation").
func TestBuildGraderPrompt_PillarName(t *testing.T) {
	prompt := buildGraderPrompt(GraderRequest{Keyword: "agent-check", Text: "the port answers"}, "check-pod", "")
	if !strings.Contains(prompt, "Agent Driven Evaluation") {
		t.Fatalf("grader prompt must name the pillar 'Agent Driven Evaluation'; got:\n%s", prompt)
	}
}
