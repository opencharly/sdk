package deploykit

// step_view.go — thin re-exports. The single InstallStep⇄InstallStepView bridge
// (StepToView / StepFromView / StepsToView + the mirror helpers) RELOCATED to
// github.com/opencharly/spec/spec (install_step_view.go, #55 step-4), beside the step
// vocabulary it type-switches. deploykit's own code + out-of-tree plugins read the
// aliases unchanged.

import "github.com/opencharly/spec/spec"

var (
	StepToView   = spec.StepToView
	StepFromView = spec.StepFromView
	StepsToView  = spec.StepsToView
)
