package deploykit

import (
	"slices"

	"github.com/opencharly/spec/spec"
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
// This pass closes that gap by injecting the ACTIVE init's depends_candy into the box's candy list,
// and it is TARGET-AWARE by construction rather than by a flag: the active init comes from
// InitConfig.ResolveInitSystem, the SAME resolution that decides ai.opencharly.init and drives the
// render. A container composition resolves to supervisord (→ the supervisord candy is injected); a
// bootc / machine-venue composition resolves to systemd (→ empty depends_candy, nothing injected).
// A blanket `require: supervisord` on every service candy would be the WRONG fix for exactly that
// reason: it is target-blind and would install supervisord onto systemd systems.
//
// WHERE IT WRITES, and why that is the whole design. A box's composition has exactly ONE authored
// home — the `candy:` list on its *spec.Config entry. buildkit.ResolveBox does not merge, inherit or
// extend it; resolved.Candy is a pure BareCandyRef normalization of the authored list
// (config_resolve.go). So ResolvedBox.Candy is a DERIVED VIEW, not a second source of truth, and this
// pass writes the SOURCE: it rewrites cfg's authored list, and every derivation — the resolved boxes,
// the base-chain collectors (CollectDescriptions / CollectBoxPorts / CollectBoxVolume / CollectHooks /
// CollectShell, all of which re-walk cfg via BoxCandyChain), BoxDirectCandies, and FillBoxPlans —
// follows for free.
//
// The first cut of this pass wrote the derived view instead (it mutated ResolvedBox.Candy), which is
// why the injected candy reached the Containerfile but contributed nothing to any collector: the
// image ran supervisord as PID 1 while the baked ai.opencharly.description label — the acceptance
// plan `charly check box` runs — never mentioned it, so the shipped init went untested. Writing the
// source is what keeps a composition-time transform from having to be re-applied per consumer.
//
// ORDERING. Because the resolved boxes derive from cfg, this pass MUST run BEFORE box resolution at
// each composition chokepoint — candy/plugin-build's resolveBuildEngine, loaderkit's
// ProjectResolvedProject, and FillNamespaceBoxViews (the namespaced set, resolved from the
// namespace's OWN config and so reached by neither of the other two). It runs after the candy scan,
// since it needs the scanned candy set to resolve the init; the remote-ref collection that reads the
// authored list (charly/refs.go) runs before the scan and is therefore untouched by construction.
//
// Placement: this needs BOTH the config and the candy graph (which candies trigger which init is a
// whole-composition question), so it cannot live in buildkit.ResolveBox — that resolves ONE box in
// isolation and deliberately carries no candy map (see buildkit.ResolveOpts' doc comment on why
// InitCfg stays out of it). It belongs here in deploykit beside ComputeIntermediates /
// GlobalCandyOrder, whose (config, layers) shape it mirrors exactly.

// InjectInitDependsCandy prepends each box's ACTIVE init system's `depends_candy` to that box's
// authored candy list, when the composition actually triggers that init and the dependency candy is
// not already in THIS BOX'S OWN resolved candy order. Mutates cfg in place; a nil cfg or initCfg is
// a no-op. It does NOT recurse cfg.Namespaces: a namespace's boxes resolve against the namespace's
// OWN scanned candy set, so FillNamespaceBoxViews applies the pass there with those layers.
//
// The scope of the presence check is deliberately the box's own order, NOT its base chain:
// ResolveCandyOrder is called with a nil parentCandies, so a child whose BASE already installs the
// init candy still has it prepended to the child's list. That is harmless for the artifact —
// GlobalOrderForBox filters a base-provided candy back out at render, so the emitted image is
// unchanged, and BoxCandyChain de-duplicates first-occurrence-wins so no collector double-counts it.
//
// The injected candy is PREPENDED, matching how boxes that list the init explicitly already author it
// (the init candy first, the service candies after) — the topological sort only constrains `require:`
// edges, so for independent candies the requested order is what survives.
//
// A box whose candy chain does not resolve is SKIPPED rather than erroring: an unresolvable chain is
// a graph defect the validator (and, on the build path, the subsequent GlobalCandyOrder) reports with
// far better context than this pass could, and turning it into a resolve abort here would break the
// error-tolerant validate/inspect projection.
//
// It is IDEMPOTENT, so running it at more than one chokepoint over the same config never
// double-injects.
func InjectInitDependsCandy(cfg *spec.Config, layers map[string]CandyModel, initCfg *spec.InitConfig) {
	if cfg == nil || initCfg == nil {
		return
	}
	for _, name := range cfg.AllBoxNames() {
		img, ok := cfg.BoxConfig(name)
		if !ok {
			continue
		}
		// The AUTHORED list carries rich refs (`@github.com/…/supervisord:2026.1.1`) while the candy
		// map is keyed bare; no normalization is needed here because ResolveCandyOrder already does
		// it at its own chokepoint (ExpandCandy BareRef-normalizes every ref before lookup).
		order, err := ResolveCandyOrder(img.Candy, layers, nil)
		if err != nil {
			continue
		}
		initName, def := initCfg.ResolveInitSystem(layers, order, img.Init)
		if def == nil || def.DependsCandy == "" {
			continue
		}
		// Already satisfied — either listed directly or pulled in transitively by a composed
		// candy's require:. Checking the RESOLVED order (not the authored list) is what makes this
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
		// A candy repo is NAMED after its repo, not its entity: ScanRemoteCandy derives a
		// root-level candy's name from path.Base(repoPath), so the standalone
		// `layer-supervisord` repo yields the name "layer-supervisord" while the entity it
		// defines — and the one `depends_candy:` names — is "supervisord". Judging
		// satisfaction by the scanned name alone therefore reported every post-cutover
		// project as unsatisfied even when it composes the candy explicitly.
		if orderSatisfiesInitDepends(order, def.DependsCandy) {
			continue
		}
		key, ok := candyKeyForName(layers, def.DependsCandy)
		if !ok {
			// The init candy is not in the project's scanned set at all. A project where it is
			// absent could not install that init by ANY route, so there is nothing to inject and
			// this pass leaves the box exactly as it found it; injecting a dangling name would
			// manufacture an "unknown candy" resolve failure that buries the project's real defect.
			//
			// Declining to inject is right. Saying nothing is not: the build then SUCCEEDS,
			// stamping this init and its entrypoint onto an image that has no binary to run,
			// and the failure surfaces only at `charly start` as a bare
			// "executable file not found in $PATH" — naming neither the init that was
			// resolved nor the candy that would have satisfied it. Everything needed to say
			// so is known right here, and the condition is decidable at BUILD time.
			reportUnsatisfiedInitDepends(name, initName, def.DependsCandy)
			continue
		}
		if slices.Contains(order, key) {
			continue
		}
		img.Candy = append([]string{key}, img.Candy...)
		cfg.SetBox(name, img)
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
