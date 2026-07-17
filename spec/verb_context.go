package spec

// verb_context.go — DoMode + ExecContext, the two check/plan vocabulary enums shared by
// every consumer that needs to classify an Op/Step (the plan walk in sdk/kit, the deploy-plan
// compiler in sdk/deploykit, charly core's VerbCatalog + registry-coupled semantics, and
// candy/plugin-box's independent validate-engine re-derivation) — moved here (K3, task #39)
// so none of them need to import a MECHANISM kit just for these types. Hand-written (not
// CUE-sourced): simple internal vocabulary enums, not an authored charly.yml surface.

// DoMode is the act/assert/instruct axis. act = perform a side-effect; assert = run the
// matchers (read-only); instruct = hand free-form text to the agent grader.
type DoMode string

const (
	DoAct      DoMode = "act"
	DoAssert   DoMode = "assert"
	DoInstruct DoMode = "instruct"
)

// StepDoMode maps the step keyword to the act/assert/instruct dispatch enum.
func StepDoMode(s *Step) DoMode {
	switch {
	case s.Run != "":
		return DoAct
	case s.Check != "":
		return DoAssert
	case s.AgentRun != "", s.AgentCheck != "":
		return DoInstruct
	}
	return DoAssert
}

// ExecContext is where an op runs. An op's Context list (or its VerbCatalog default)
// declares legality; the active engine supplies the running context and skips ops whose
// context set does not include it.
type ExecContext string

const (
	CtxBuild   ExecContext = "build"
	CtxDeploy  ExecContext = "deploy"
	CtxRuntime ExecContext = "runtime"
)
