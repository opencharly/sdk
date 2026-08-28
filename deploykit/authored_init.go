package deploykit

import "github.com/opencharly/spec/spec"

// AuthoredInit returns the init system a box explicitly declares via `#Box.init`,
// or "" when it declares none (the auto-detect case).
//
// The field has been declared in the schema, generated into BoxYAML.Init and read
// by NOTHING: every ResolveInitSystem call site passed a hardcoded "" while the
// parameter was fully implemented with a correct fall-through, so authoring
// `init: systemd` on a box silently did nothing. This is the reader that makes the
// declaration load-bearing.
//
// Namespace-aware: boxName may be qualified, so the lookup goes through BoxOwner
// rather than indexing the config directly.
func AuthoredInit(cfg *spec.Config, boxName string) string {
	if cfg == nil {
		return ""
	}
	_, _, imgCfg, ok := BoxOwner(cfg, boxName)
	if !ok {
		return ""
	}
	return imgCfg.Init
}

// fragmentRenderContext returns the half of a ServiceRenderContext that
// BuildServiceRenderContext cannot derive from the entry: which candy the entry came
// from, and where a rendered unit lands.
//
// It is deliberately the ONLY thing the build path seeds. BuildServiceRenderContext
// overwrites every entry-derived field and APPENDS After/Before, so seeding those
// was dead at best and double-listed ordering directives at worst. Mirrors
// compile_service_steps.go's deploy-time context.
func fragmentRenderContext(candyName string, img *ResolvedBox) spec.ServiceRenderContext {
	ctx := spec.ServiceRenderContext{
		Candy:         candyName,
		SystemUnitDir: "/etc/systemd/system",
	}
	// Without Home the home-expansion pass is skipped and `%(ENV_HOME)s` reaches a
	// systemd unit verbatim; supervisord expands it natively, which is why the gap
	// stayed invisible on the only init path that had consumers.
	if img != nil && img.Home != "" {
		ctx.Home = img.Home
		ctx.UserUnitDir = img.Home + "/.config/systemd/user"
	}
	return ctx
}
