package workflowkit

import (
	"encoding/json"
	"os"
	"strings"
)

// template.go — the `${arg}` / `$VAR` substitution helper plus the LOBSTER_ARG_*
// naming. Both are shared: the task runner expands `${VAR}` in a task's dir/command,
// and the lowering names a workflow's resolved args exactly as upstream lobster
// does, so a lowered workflow's env and a natively-run lobster workflow agree.

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

// LobsterArgName normalizes a workflow arg name to upstream lobster's env-var
// spelling: uppercased, every non-alphanumeric character replaced by `_`.
// `my-arg` → `MY_ARG`; the env var is `LOBSTER_ARG_MY_ARG`.
func LobsterArgName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// LobsterArgEnvVar returns the env var upstream lobster exposes for one resolved
// arg: `LOBSTER_ARG_<NAME>`.
func LobsterArgEnvVar(name string) string { return "LOBSTER_ARG_" + LobsterArgName(name) }

// LobsterArgsEnv renders a resolved arg set into the environment upstream lobster
// builds for a workflow step: one `LOBSTER_ARG_<NAME>` per arg plus the whole set
// as `LOBSTER_ARGS_JSON`. The JSON is deterministic (Go map marshalling sorts keys).
func LobsterArgsEnv(args map[string]string) map[string]string {
	out := make(map[string]string, len(args)+1)
	for name, v := range args {
		out[LobsterArgEnvVar(name)] = v
	}
	if b, err := json.Marshal(args); err == nil {
		out["LOBSTER_ARGS_JSON"] = string(b)
	}
	return out
}
