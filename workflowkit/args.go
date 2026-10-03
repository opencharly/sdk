package workflowkit

import (
	"fmt"
	"path/filepath"

	"github.com/opencharly/spec/spec"
)

// args.go — task-argument resolution and workdir resolution, moved VERBATIM from
// plugin-task's resolveParams / resolveTaskDir (R3) so the plugin-task CLI and the
// workflow lowering share ONE implementation.

// ResolveArgs overlays a task's declared `params:` defaults with the CLI-supplied
// overrides, and enforces `required: true`. A required param with neither an
// override nor a default is a hard error. An undeclared CLI arg passes through
// (forward-compat, and vars-like usage).
func ResolveArgs(t spec.Task, cli map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for name, ps := range t.Params {
		if v, ok := cli[name]; ok {
			out[name] = v
			continue
		}
		if ps.Default != nil {
			out[name] = fmt.Sprint(ps.Default)
			continue
		}
		if ps.Required {
			return nil, fmt.Errorf("required parameter %q not supplied (--param %s=VALUE)", name, name)
		}
	}
	// Pass through any CLI param not declared (forward-compat, and vars-like usage).
	for k, v := range cli {
		if _, declared := t.Params[k]; !declared {
			out[k] = v
		}
	}
	return out, nil
}

// ResolveTaskDir resolves a task's dir against the project root, expanding ${VAR} /
// $VAR references against the merged task env (so `dir: "$HOME/src"` and
// `dir: "${WORKDIR}/src"` both work). An empty dir is the project root.
func ResolveTaskDir(projDir, dir string, env map[string]string) string {
	if dir == "" {
		return projDir
	}
	dir = ExpandVars(dir, env)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projDir, dir)
	}
	return dir
}

// MergedEnv is the variable map every step + the dir resolution sees: the task's
// vars, then its env, then the RESOLVED args. Later sources win on a key collision.
// Moved from plugin-task's mergedEnv (R3).
func MergedEnv(t spec.Task, args map[string]string) map[string]string {
	env := map[string]string{}
	for k, v := range t.Vars {
		env[k] = v
	}
	for k, v := range t.Env {
		env[k] = v
	}
	for k, v := range args {
		env[k] = v
	}
	return env
}
