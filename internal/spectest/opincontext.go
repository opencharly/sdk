// Package spectest holds the sdk-INTERNAL, test-only helpers that more than one sdk package's
// test binary needs.
//
// It is a package rather than a per-file copy because R3 (one canonical implementation per
// behaviour — charly/AGENTS.md) admits no "a second copy is fine" threshold: the op-context
// classifier below is a pure ~15-line function, but it was carried as an independent port in
// BOTH deploykit's and loaderkit's test binaries, and a divergence between the two would make
// two sdk packages disagree about which ops run where. Under internal/ it stays unimportable
// from outside the module, so it adds nothing to the sdk's published surface.
package spectest

import (
	"slices"

	"github.com/opencharly/spec/spec"
)

// OpEffectiveContexts returns the execution context(s) an op actually runs in: the op's OWN
// declared `context:` when it declares any, else the verb's catalog default.
//
// spec.OpInContext is a package-level DI hook (spec/spec/injection_seams.go) that production
// charly core wires from its own init() (charly/charly/layers.go: spec.OpInContext =
// opInContext), safe there because charly always shares that process with the compile/scan
// call. An sdk-only test binary links no charly core, so the hook stays nil and the first call
// to it panics. The classifier is PURE — spec.VerbCatalog is static data plus the op's own
// declared Context, no registry consult — so it is ported verbatim from
// charly/planrun_adapter.go's opInContext/opEffectiveContexts.
func OpEffectiveContexts(c *spec.Op) []spec.ExecContext {
	if len(c.Context) > 0 {
		out := make([]spec.ExecContext, 0, len(c.Context))
		for _, s := range c.Context {
			out = append(out, spec.ExecContext(s))
		}
		return out
	}
	if verb, err := c.Kind(); err == nil {
		if vs, ok := spec.VerbCatalog[verb]; ok {
			return vs.Contexts
		}
	}
	return nil
}

// OpInContext reports whether the op runs in ctx, by the same rule as OpEffectiveContexts.
func OpInContext(c *spec.Op, ctx spec.ExecContext) bool {
	return slices.Contains(OpEffectiveContexts(c), ctx)
}

// Install wires the spec.OpInContext DI hook to this package's port. Call it from a test
// binary's init(); calling it more than once is a harmless no-op (it assigns the same func).
func Install() {
	spec.OpInContext = OpInContext
}
