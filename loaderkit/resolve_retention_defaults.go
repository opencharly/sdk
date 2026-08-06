package loaderkit

import (
	"context"

	"github.com/opencharly/sdk"
)

// resolve_retention_defaults.go — the ONE shared plugin-side resolution of the project's
// defaults.keep_images / defaults.keep_check_runs, reached over the reverse channel.
// K-wave 2 cone R6: the loader is plugin-reachable (LoadUnifiedViaExecutor), so the former
// "retention-defaults" HostBuild seam (charly/host_build_retention_defaults.go, DELETED) is gone
// — every verb:retention caller resolves the tunables itself. The three consumers
// (candy/plugin-clean's own CLI, candy/plugin-check's post-run prune, candy/plugin-box's
// post-build prune) share this ONE function (R3 — no duplicated LoadConfig projection across
// three plugin modules). An absent / load-failing project degrades to 0/0 ("retention disabled"),
// matching the deleted seam's best-effort contract; ex nil (a placement without a reverse
// channel, e.g. the out-of-process CliMain path) degrades the same way — callers that must error
// on that placement guard for ex == nil themselves.
func ResolveRetentionDefaultsViaExecutor(ctx context.Context, ex *sdk.Executor, dir string) (keepImages, keepCheckRuns int) {
	if ex == nil {
		return 0, 0
	}
	uf, present, err := LoadUnifiedViaExecutor(ctx, ex, dir)
	if err != nil || !present || uf == nil {
		return 0, 0
	}
	if cfg := uf.ProjectConfig(); cfg != nil {
		if cfg.Defaults.KeepImages != nil {
			keepImages = *cfg.Defaults.KeepImages
		}
		if cfg.Defaults.KeepCheckRuns != nil {
			keepCheckRuns = *cfg.Defaults.KeepCheckRuns
		}
	}
	return keepImages, keepCheckRuns
}
