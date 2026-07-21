package kit

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/sdk/spec"
)

func TestStatus_String(t *testing.T) {
	for _, tc := range []struct {
		s    Status
		want string
	}{
		{StatusPass, "pass"},
		{StatusFail, "fail"},
		{StatusSkip, "skip"},
		{Status(99), "unknown"},
	} {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Status(%d).String() = %q, want %q", int(tc.s), got, tc.want)
		}
	}
}

func TestPlural(t *testing.T) {
	if Plural(1) != "" {
		t.Error("Plural(1) must be empty")
	}
	for _, n := range []int{0, 2, 17} {
		if Plural(n) != "s" {
			t.Errorf("Plural(%d) must be \"s\"", n)
		}
	}
}

func sampleResults() []StepResult {
	return []StepResult{
		{Keyword: "check", Text: "the marker exists", Origin: "layer-a", StepID: "a-marker",
			Result: CheckResult{CheckResult: spec.CheckResult{Verb: "file", Status: StatusPass, Message: "found", Elapsed: 12 * time.Millisecond}}},
		{Keyword: "check", Text: "the port is open", Origin: "layer-a", StepID: "a-port",
			Result: CheckResult{CheckResult: spec.CheckResult{Verb: "port", Status: StatusFail, Message: "connection refused", Elapsed: 5 * time.Millisecond}}},
		{Keyword: "check", Text: "the gpu is present", Origin: "layer-b", StepID: "b-gpu",
			Result: CheckResult{CheckResult: spec.CheckResult{Verb: "command", Status: StatusSkip, Message: "no gpu vendor", Elapsed: time.Millisecond}}},
	}
}

// TestFormatStepResultsText pins the exact human-readable output, incl. the summary line
// and the uppercased status column. This is the format the check-live golden oracle compares.
func TestFormatStepResultsText(t *testing.T) {
	var b bytes.Buffer
	FormatStepResultsText(&b, sampleResults())
	out := b.String()

	for _, want := range []string{
		"  PASS  check the marker exists  [a-marker]  found",
		"  FAIL  check the port is open  [a-port]  connection refused",
		"  SKIP  check the gpu is present  [b-gpu]  no gpu vendor",
		"\n3 steps: 1 passed, 1 failed, 1 skipped\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n---\n%s", want, out)
		}
	}

	// Singular summary uses the empty plural.
	var one bytes.Buffer
	FormatStepResultsText(&one, sampleResults()[:1])
	if !strings.Contains(one.String(), "1 step: 1 passed, 0 failed, 0 skipped") {
		t.Errorf("singular summary wrong:\n%s", one.String())
	}
}

// TestRenderStep_RetryInfo pins the attempts/elapsed suffix a retried step gets.
func TestRenderStep_RetryInfo(t *testing.T) {
	var b bytes.Buffer
	FormatStepResultsText(&b, []StepResult{{
		Keyword: "check", Text: "eventually up", StepID: "e1",
		Result: CheckResult{CheckResult: spec.CheckResult{Status: StatusPass, Attempts: 5, TotalElapsed: 12300 * time.Millisecond}},
	}})
	if !strings.Contains(b.String(), "(attempts=5, elapsed=12.3s)") {
		t.Errorf("retry info missing:\n%s", b.String())
	}
	// A single-attempt step carries NO retry suffix.
	var one bytes.Buffer
	FormatStepResultsText(&one, []StepResult{{
		Keyword: "check", Text: "once", StepID: "o1",
		Result: CheckResult{CheckResult: spec.CheckResult{Status: StatusPass, Attempts: 1}},
	}})
	if strings.Contains(one.String(), "attempts=") {
		t.Errorf("single-attempt step must not show retry info:\n%s", one.String())
	}
}

func TestFormatStepResultsTAP(t *testing.T) {
	var b bytes.Buffer
	FormatStepResultsTAP(&b, sampleResults())
	out := b.String()
	for _, want := range []string{
		"TAP version 13\n",
		"1..3\n",
		"ok 1 - check the marker exists\n",
		"not ok 2 - check the port is open\n",
		"  verb: \"port\"\n",
		"  message: \"connection refused\"\n",
		"ok 3 - check the gpu is present\n", // skip is still "ok" in TAP
	} {
		if !strings.Contains(out, want) {
			t.Errorf("TAP output missing %q\n---\n%s", want, out)
		}
	}
	// The failing point emits a YAML diagnostic block; passing/skip points do not.
	if strings.Count(out, "  ---\n") != 1 {
		t.Errorf("exactly one TAP diagnostic block expected (the one failure):\n%s", out)
	}
}

func TestFormatStepResultsJSON_RoundTrips(t *testing.T) {
	var b bytes.Buffer
	if err := FormatStepResultsJSON(&b, sampleResults()); err != nil {
		t.Fatal(err)
	}
	var back []StepResult
	if err := json.Unmarshal(b.Bytes(), &back); err != nil {
		t.Fatalf("JSON did not round-trip: %v\n%s", err, b.String())
	}
	if len(back) != 3 || back[1].Result.Status != StatusFail || back[1].StepID != "a-port" {
		t.Errorf("round-tripped results wrong: %+v", back)
	}
}

func TestFormatStepResultsJUnit(t *testing.T) {
	var b bytes.Buffer
	if err := FormatStepResultsJUnit(&b, sampleResults()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("JUnit output must start with the XML declaration:\n%s", out)
	}

	type tc struct {
		Name    string `xml:"name,attr"`
		Failure *struct {
			Message string `xml:"message,attr"`
		} `xml:"failure"`
		Skipped *struct{} `xml:"skipped"`
	}
	type ts struct {
		Name     string `xml:"name,attr"`
		Tests    int    `xml:"tests,attr"`
		Failures int    `xml:"failures,attr"`
		Skipped  int    `xml:"skipped,attr"`
		Cases    []tc   `xml:"testcase"`
	}
	var doc struct {
		Suites []ts `xml:"testsuite"`
	}
	if err := xml.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatalf("JUnit XML did not parse: %v\n%s", err, out)
	}
	// Two origins → two suites, grouped and first-seen ordered.
	if len(doc.Suites) != 2 || doc.Suites[0].Name != "layer-a" || doc.Suites[1].Name != "layer-b" {
		t.Fatalf("suites wrong: %+v", doc.Suites)
	}
	if doc.Suites[0].Tests != 2 || doc.Suites[0].Failures != 1 || doc.Suites[0].Skipped != 0 {
		t.Errorf("layer-a suite counts wrong: %+v", doc.Suites[0])
	}
	if doc.Suites[1].Tests != 1 || doc.Suites[1].Skipped != 1 {
		t.Errorf("layer-b suite counts wrong: %+v", doc.Suites[1])
	}
}

// TestStepResultCarriesOp confirms the moved CheckResult keeps the *spec.Op back-reference
// (a plugin reporting results needs to reach the Op that produced them).
func TestStepResultCarriesOp(t *testing.T) {
	op := &spec.Op{}
	sr := StepResult{Result: CheckResult{CheckResult: spec.CheckResult{Op: op, Status: StatusPass}}}
	if sr.Result.Op != op {
		t.Error("CheckResult.Op back-reference lost")
	}
}
