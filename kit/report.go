package kit

// report.go — re-export of the check-run result reporters, RELOCATED to the spec/report fabric
// slice github.com/opencharly/spec/report/report.go (#55 CHECK-ENGINE cone Option A — the
// FormatStepResults* family + ReportStepResultsCount + ClassifyStepFailures charly core's
// deploy-verify path (check_cmd.go's checkLocalDeployScope) reaches importing zero kit). The
// reporters operate on []spec.StepResult + spec.Status; the INFRA-failure discriminator
// IsContainerInfraResult lives in spec/exec (a host primitive), so the body homes in spec/report
// (its own package, to import spec/exec WITHOUT forcing spec/spec to cycle). kit re-exports the
// symbols here so every existing kit.Plural / kit.FormatStepResults* / kit.ReportStepResults /
// kit.ReportStepResultsCount / kit.ClassifyStepFailures call site (charly core + the candies +
// sdk) is untouched. New consumers should import spec/report directly.

import "github.com/opencharly/spec/report"

// Plural returns "" for n==1 and "s" otherwise — the trivial English pluralization used by the
// result reporters and the eventually-retry summaries. Re-exported from report.Plural.
var Plural = report.Plural

// FormatStepResultsText emits a human-readable per-step report to w. Re-exported from
// report.FormatStepResultsText.
var FormatStepResultsText = report.FormatStepResultsText

// FormatStepResultsJSON emits a structured JSON document. Re-exported from
// report.FormatStepResultsJSON.
var FormatStepResultsJSON = report.FormatStepResultsJSON

// FormatStepResultsTAP emits TAP v13. Re-exported from report.FormatStepResultsTAP.
var FormatStepResultsTAP = report.FormatStepResultsTAP

// FormatStepResultsJUnit emits JUnit XML for CI dashboards. Re-exported from
// report.FormatStepResultsJUnit.
var FormatStepResultsJUnit = report.FormatStepResultsJUnit

// ReportStepResults writes results in the requested format. Re-exported from
// report.ReportStepResults.
var ReportStepResults = report.ReportStepResults

// ReportStepResultsCount renders results per format via ReportStepResults and returns how many
// results ended in a FAIL verdict. Re-exported from report.ReportStepResultsCount.
var ReportStepResultsCount = report.ReportStepResultsCount

// ClassifyStepFailures splits FAIL results into genuine check failures vs. container-setup
// INFRA failures. Re-exported from report.ClassifyStepFailures.
var ClassifyStepFailures = report.ClassifyStepFailures
