package workflowkit

import (
	"path/filepath"
)

// args.go — the KIND-BLIND workdir resolution: it takes strings, knows no task body, and
// is shared by every caller that resolves an authored directory against a project root.
//
// The task-shaped helpers that used to live here — `ResolveArgs` and `MergedEnv` — moved
// HOME to `plugin-task` (opencharly/spec#304 unit 2). They were here because the SDK's
// pipeline lowering was their second caller (ONE implementation for the plugin-task CLI
// and the lowering, plugin-task CHANGELOG 2026.277.0711); unit 1 moved that lowering into
// the plugin that owns the pipeline kind, so the second caller is gone — and a helper
// typed on `spec.Task` cannot stay in a kind-blind mechanism library once the task body
// lives with its own plugin.

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
