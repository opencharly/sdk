package kit

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// Plural returns "" for n==1 and "s" otherwise — the trivial English pluralization used by
// the result reporters and the eventually-retry summaries.
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// FormatStepResultsText emits a human-readable per-step report to w.
func FormatStepResultsText(w io.Writer, results []StepResult) {
	var passed, failed, skipped int
	for i := range results {
		step := results[i]
		switch step.Result.Status {
		case StatusFail:
			failed++
		case StatusSkip:
			skipped++
		default:
			passed++
		}
		renderStep(w, &step)
	}
	fmt.Fprintf(w, "\n%d step%s: %d passed, %d failed, %d skipped\n",
		len(results), Plural(len(results)), passed, failed, skipped)
}

func renderStep(w io.Writer, step *StepResult) {
	status := strings.ToUpper(step.Result.Status.String())
	// A podman container-SETUP infra failure (R44) is NOT a checks-failed verdict — label it
	// INFRA so a recurring infra storm is loud and distinguishable from a real FAIL at a glance.
	if step.Result.Status == StatusFail && IsContainerInfraResult(step.Result.Message) {
		status = "INFRA"
	}
	retryInfo := ""
	if step.Result.Attempts > 1 {
		retryInfo = fmt.Sprintf(" (attempts=%d, elapsed=%s)",
			step.Result.Attempts, step.Result.TotalElapsed.Round(time.Millisecond))
	}
	msg := step.Result.Message
	if msg != "" {
		msg = "  " + msg
	}
	fmt.Fprintf(w, "  %-5s %s %s  [%s]%s%s\n",
		status, step.Keyword, step.Text, step.StepID, retryInfo, truncate(msg, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// FormatStepResultsJSON emits a structured JSON document.
func FormatStepResultsJSON(w io.Writer, results []StepResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// FormatStepResultsTAP emits TAP v13. Each step is one TAP test point.
func FormatStepResultsTAP(w io.Writer, results []StepResult) {
	fmt.Fprintln(w, "TAP version 13")
	fmt.Fprintf(w, "1..%d\n", len(results))
	for i, step := range results {
		directive := "ok"
		if step.Result.Status == StatusFail {
			directive = "not ok"
		}
		fmt.Fprintf(w, "%s %d - %s %s\n", directive, i+1, step.Keyword, step.Text)
		if step.Result.Status == StatusFail {
			fmt.Fprintln(w, "  ---")
			fmt.Fprintf(w, "  origin: %q\n", step.Origin)
			fmt.Fprintf(w, "  step_id: %q\n", step.StepID)
			fmt.Fprintf(w, "  verb: %q\n", step.Result.Verb)
			fmt.Fprintf(w, "  message: %q\n", step.Result.Message)
			fmt.Fprintln(w, "  ...")
		}
	}
}

// FormatStepResultsJUnit emits JUnit XML for CI dashboards. Steps surface as
// <testcase> grouped by origin into <testsuite>s.
func FormatStepResultsJUnit(w io.Writer, results []StepResult) error {
	type junitFailure struct {
		Message string `xml:"message,attr"`
		Body    string `xml:",chardata"`
	}
	type junitSkipped struct {
		Message string `xml:"message,attr"`
	}
	type junitTestCase struct {
		XMLName   xml.Name      `xml:"testcase"`
		Name      string        `xml:"name,attr"`
		Classname string        `xml:"classname,attr"`
		Time      float64       `xml:"time,attr"`
		Failure   *junitFailure `xml:"failure,omitempty"`
		Skipped   *junitSkipped `xml:"skipped,omitempty"`
	}
	type junitTestSuite struct {
		XMLName  xml.Name        `xml:"testsuite"`
		Name     string          `xml:"name,attr"`
		Tests    int             `xml:"tests,attr"`
		Failures int             `xml:"failures,attr"`
		Skipped  int             `xml:"skipped,attr"`
		Time     float64         `xml:"time,attr"`
		Cases    []junitTestCase `xml:"testcase"`
	}
	type junitTestSuites struct {
		XMLName xml.Name         `xml:"testsuites"`
		Suites  []junitTestSuite `xml:"testsuite"`
	}

	// Group steps by origin (preserving first-seen order).
	var order []string
	byOrigin := map[string]*junitTestSuite{}
	for i := range results {
		step := results[i]
		suite := byOrigin[step.Origin]
		if suite == nil {
			suite = &junitTestSuite{Name: step.Origin}
			byOrigin[step.Origin] = suite
			order = append(order, step.Origin)
		}
		elapsed := step.Result.Elapsed.Seconds()
		if step.Result.TotalElapsed > 0 {
			elapsed = step.Result.TotalElapsed.Seconds()
		}
		tc := junitTestCase{
			Name:      step.Keyword + " " + step.Text,
			Classname: step.Origin,
			Time:      elapsed,
		}
		switch step.Result.Status {
		case StatusFail:
			tc.Failure = &junitFailure{
				Message: step.Result.Message,
				Body:    "Verb: " + step.Result.Verb + "\nStep ID: " + step.StepID,
			}
			suite.Failures++
		case StatusSkip:
			tc.Skipped = &junitSkipped{Message: step.Result.Message}
			suite.Skipped++
		}
		suite.Cases = append(suite.Cases, tc)
		suite.Time += elapsed
	}

	var suites junitTestSuites
	for _, o := range order {
		s := byOrigin[o]
		s.Tests = len(s.Cases)
		suites.Suites = append(suites.Suites, *s)
	}

	fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(suites); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return nil
}

// ReportStepResults writes results in the requested format ("json"/"tap"/"junit", default
// text), dispatching to the FormatStepResults* family above. P12a follow-up: the SAME
// format-selection switch previously lived duplicated in charly/check_feature_run.go's
// reportSteps AND candy/plugin-check/check_cmd.go's reportSteps (R3) — both now delegate here,
// and candy/plugin-box's `box feature run` word (its move destination) calls it directly.
func ReportStepResults(w io.Writer, results []StepResult, format string) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		_ = FormatStepResultsJSON(w, results)
	case "tap":
		FormatStepResultsTAP(w, results)
	case "junit":
		_ = FormatStepResultsJUnit(w, results)
	default:
		FormatStepResultsText(w, results)
	}
}

// ReportStepResultsCount renders results per format via ReportStepResults and returns how many
// results ended in a FAIL verdict (no infra/check split — a caller needing that split uses
// ClassifyStepFailures directly). Moved from charly/check_feature_run.go's
// reportSteps+stepFailCount (CHECK-wave) — that pair had DRIFTED from this file's own comment
// above (which already claimed "both [check_cmd.go and check_feature_run.go] now delegate
// here"): check_feature_run.go's reportSteps still re-implemented the same format switch instead
// of calling ReportStepResults. Both callers already imported kit and had zero core-state
// coupling — a plain report+count wrapper.
func ReportStepResultsCount(w io.Writer, results []StepResult, format string) int {
	ReportStepResults(w, results, format)
	fails := 0
	for i := range results {
		if results[i].Result.Status == StatusFail {
			fails++
		}
	}
	return fails
}

// ClassifyStepFailures splits FAIL results into genuine check failures vs. container-setup
// INFRA failures (IsContainerInfraResult) — the shared discriminator every check-run caller
// (box/live/feature, in-core and externalized) uses to map its own exit-code error type. An
// infra failure means the check command never ran (a probe container's mount/passwd-gen raced
// concurrent build churn); it must never read as a checks-failed verdict.
func ClassifyStepFailures(results []StepResult) (checkFails, infraFails int) {
	for i := range results {
		if results[i].Result.Status != StatusFail {
			continue
		}
		if IsContainerInfraResult(results[i].Result.Message) {
			infraFails++
		} else {
			checkFails++
		}
	}
	return checkFails, infraFails
}
