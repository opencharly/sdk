package deploykit

import (
	"slices"

	"github.com/opencharly/sdk/buildkit"
)

// init_depends.go — the init `depends_candy:` INJECTION pass.
//
// The init build-vocabulary (`charly/charly.yml` `init:`) declares, per init system, the candy that
// installs that init's own runtime: `supervisord` → depends_candy: supervisord (a container carries
// no init, so it must be installed); `systemd` → NO depends_candy (already present on every machine
// venue). Declaring a `service:` in a candy is what SELECTS an init and assembles its config — but
// nothing used to INSTALL the selected init's binary, so a box composing only service candies built
// and validated clean, baked ai.opencharly.init="supervisord" and an assembled /etc/supervisord.conf,
// and then failed at deploy `[start]` because /usr/bin/supervisord did not exist.
//
// This pass closes that gap by injecting the ACTIVE init's depends_candy into the box's own candy
// list, and it is TARGET-AWARE by construction rather than by a flag: the active init comes from
// InitConfig.ResolveInitSystem, the SAME resolution that decides ai.opencharly.init and drives the
// render. A container composition resolves to supervisord (→ the supervisord candy is injected); a
// bootc / machine-venue composition resolves to systemd (→ empty depends_candy, nothing injected).
// A blanket `require: supervisord` on every service candy would be the WRONG fix for exactly that
// reason: it is target-blind and would install supervisord onto systemd systems.
//
// Placement: this needs BOTH the box set and the candy graph (which candies trigger which init is a
// whole-composition question), so it cannot live in buildkit.ResolveBox — that resolves ONE box in
// isolation and deliberately carries no candy map (see buildkit.ResolveOpts' doc comment on why
// InitCfg stays out of it). It belongs here in deploykit beside ComputeIntermediates /
// GlobalCandyOrder, whose (boxes, layers) shape it mirrors exactly.
//
// It is called from the TWO box-resolution chokepoints — candy/plugin-build's resolveBuildEngine
// (after buildkit.ResolveAllBox, BEFORE ComputeIntermediates/GlobalCandyOrder/RenderPrepAll, so the
// emitted Containerfile and the baked labels both see it) and loaderkit.ProjectResolvedProject's
// fresh-box path (which feeds the validate / inspect / bundle-deploy envelope). It is IDEMPOTENT, so
// the build path's pre-resolved boxes flowing through the envelope assembler are never double-injected.

// InjectInitDependsCandy appends each box's ACTIVE init system's `depends_candy` to that box's candy
// list, when the composition actually triggers that init and the dependency candy is not already in
// THIS BOX'S OWN resolved candy order. Mutates the boxes in place; a nil initCfg is a no-op.
//
// The scope of that check is deliberately the box's own order, NOT its base chain: ResolveCandyOrder
// is called with a nil parentCandies, so a child whose BASE already installs the init candy still has
// it prepended to the child's list. That is harmless for the artifact — GlobalOrderForBox filters a
// base-provided candy back out at render, so the emitted image is unchanged — but it does mean
// rp.Boxes[child].Candy can name a candy the child itself does not install. Anything reading that
// field as "what this box installs" rather than "what this box requested" must account for it.
//
// The injected candy is PREPENDED, matching how boxes that list the init explicitly already author it
// (the init candy first, the service candies after) — the topological sort only constrains `require:`
// edges, so for independent candies the requested order is what survives.
//
// A box whose candy chain does not resolve is SKIPPED rather than erroring: an unresolvable chain is
// a graph defect the validator (and, on the build path, the subsequent GlobalCandyOrder) reports with
// far better context than this pass could, and turning it into a resolve abort here would break the
// error-tolerant validate/inspect projection.
func InjectInitDependsCandy(boxes map[string]*buildkit.ResolvedBox, layers map[string]CandyModel, initCfg *buildkit.InitConfig) {
	if initCfg == nil {
		return
	}
	for _, img := range boxes {
		if img == nil {
			continue
		}
		order, err := ResolveCandyOrder(img.Candy, layers, nil)
		if err != nil {
			continue
		}
		_, def := initCfg.ResolveInitSystem(layers, order, "")
		if def == nil || def.DependsCandy == "" {
			continue
		}
		// Already satisfied — either listed directly or pulled in transitively by a composed
		// candy's require:. Checking the RESOLVED order (not img.Candy) is what makes this
		// idempotent and keeps a box that already depends on the init from gaining a duplicate.
		if slices.Contains(order, def.DependsCandy) {
			continue
		}
		// Inject the candy's own MAP KEY, which is NOT always its name: BareCandyRef strips only the
		// `@` and the `:version` suffix, so a REMOTE candy is keyed by its full repo path
		// (`github.com/opencharly/charly/candy/supervisord`) while the init vocabulary names it bare
		// (`supervisord`). Resolving by key alone silently skipped every distro submodule, where the
		// init candy is always a remote `@github…` ref — caught by reading the emitted Containerfile,
		// not by the unit tests, which model a local project. Matching on GetName() is what the
		// former validator did for the same reason.
		key, ok := candyKeyForName(layers, def.DependsCandy)
		if !ok {
			// The init candy is not in the project's scanned set at all. A project where it is
			// absent could not install that init by ANY route, so there is nothing to inject and
			// this pass leaves the box exactly as it found it; injecting a dangling name would
			// manufacture an "unknown candy" resolve failure that buries the project's real defect.
			continue
		}
		if slices.Contains(order, key) {
			continue
		}
		img.Candy = append([]string{key}, img.Candy...)
	}
}

// candyKeyForName finds the candy-map KEY for a bare candy name. A LOCAL candy is keyed by its bare
// name, so the direct hit answers; a REMOTE candy is keyed by its full repo path, so the fallback
// scans for the entry whose model name matches. The scan is deterministic despite ranging a map:
// candy names are globally unique, so at most one entry can match.
func candyKeyForName(layers map[string]CandyModel, name string) (string, bool) {
	if _, ok := layers[name]; ok {
		return name, true
	}
	for key, m := range layers {
		if m != nil && m.GetName() == name {
			return key, true
		}
	}
	return "", false
}
