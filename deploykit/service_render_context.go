package deploykit

import (
	"maps"
	"sort"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// service_render_context.go — BuildServiceRenderContext, promoted from charly/service_render.go
// (K3 render-seam production move). A PURE spec.ServiceEntry projection (no init-system
// knowledge, no charly-core state): both charly's own RenderService (still the DEPLOY-mode path,
// install_build_services.go) and candy/plugin-build's render (the BUILD-mode render-seam,
// eliminated by this move) call this ONE shared source (R3) — the packaged/drop-in branch
// decisions the plugin renders from are precomputed identically either way.

// FlattenedEnvMap merges an entry's base env with its overrides' env (overrides win).
func FlattenedEnvMap(base map[string]string, overrides *spec.CandyServiceOverrides) map[string]string {
	out := make(map[string]string, len(base))
	maps.Copy(out, base)
	if overrides != nil {
		maps.Copy(out, overrides.Env)
	}
	return out
}

// SortedEnvList returns env as a deterministically-ordered []spec.KeyValue (template
// iteration order — a Go map has none of its own).
func SortedEnvList(env map[string]string) []spec.KeyValue {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]spec.KeyValue, 0, len(keys))
	for _, k := range keys {
		out = append(out, spec.KeyValue{Key: k, Value: env[k]})
	}
	return out
}

// BuildServiceRenderContext fills the entry-derived, home-expanded render context (a pure
// spec.ServiceEntry projection — no init-system knowledge). The plugin renders its templates
// against this; the packaged/drop-in branch decisions are precomputed here (PackagedUnit,
// RenderDropin) so the plugin renders from the ctx alone.
func BuildServiceRenderContext(entry *spec.ServiceEntry, ctx spec.ServiceRenderContext) spec.ServiceRenderContext {
	ctx.Name = entry.Name
	ctx.Scope = entry.EffectiveScope()
	ctx.PackagedUnit = entry.UsePackaged
	ctx.RenderDropin = entry.Overrides != nil
	ctx.Env = FlattenedEnvMap(entry.Env, entry.Overrides)
	ctx.EnvList = SortedEnvList(ctx.Env)
	if entry.Exec != "" {
		ctx.Exec = entry.Exec
	}
	if entry.Overrides != nil && entry.Overrides.Exec != "" {
		ctx.Exec = entry.Overrides.Exec
	}
	if entry.WorkingDirectory != "" {
		ctx.WorkingDirectory = entry.WorkingDirectory
	}
	// Make home-relative exec/working-dir/env portable across init systems (supervisord's
	// %(ENV_HOME)s + ~ / ${HOME} / $HOME), resolved against ctx.Home.
	if ctx.Home != "" {
		homify := func(s string) string {
			s = strings.ReplaceAll(s, "%(ENV_HOME)s", ctx.Home)
			return kit.ExpandPath(s, ctx.Home)
		}
		ctx.Exec = homify(ctx.Exec)
		ctx.WorkingDirectory = homify(ctx.WorkingDirectory)
		for k, v := range ctx.Env {
			ctx.Env[k] = homify(v)
		}
		ctx.EnvList = SortedEnvList(ctx.Env)
	}
	if entry.User != "" {
		ctx.User = entry.User
	}
	ctx.After = append(ctx.After, entry.After...)
	if entry.Overrides != nil {
		ctx.After = append(ctx.After, entry.Overrides.After...)
	}
	ctx.Before = append(ctx.Before, entry.Before...)
	ctx.WantedBy = entry.WantedBy
	ctx.Restart = entry.Restart
	ctx.Stdout = entry.Stdout
	ctx.StopTimeout = entry.StopTimeout
	ctx.Kind = entry.Kind
	ctx.Events = entry.Events
	ctx.AutoStart = entry.AutoStart
	ctx.StartRetries = entry.StartRetries
	ctx.StartSecs = entry.StartSecs
	ctx.StopSignal = entry.StopSignal
	ctx.ExitCodes = entry.ExitCode
	ctx.Priority = entry.Priority
	return ctx
}
