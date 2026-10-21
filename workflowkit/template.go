package workflowkit

import (
	"os"
)

// template.go — the `${arg}` / `$VAR` substitution helper. The task runner expands
// `${VAR}` in a task's dir/command, and the workflow lowering emits `REF_n` refs into
// a step's env for the same expansion.
//
// The LOBSTER_ARG_* / LOBSTER_ARGS_JSON env projection is NOT here: a resolved arg
// set becomes environment in the engine that runs the workflow (plugin-lobster's
// `mergeEnv`), the ONE implementation of that projection and the only one that sees
// the arg values the wire actually admits.

// ExpandVars substitutes ${VAR} and $VAR using env first, then the process
// environment — so a task's dir/command can reference HOME, the project vars, and
// resolved params uniformly. Moved VERBATIM from plugin-task's expandVars (R3: one
// implementation, its callers repointed).
func ExpandVars(s string, env map[string]string) string {
	return os.Expand(s, func(key string) string {
		if v, ok := env[key]; ok {
			return v
		}
		return os.Getenv(key)
	})
}
