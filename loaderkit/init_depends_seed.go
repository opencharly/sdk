package loaderkit

import (
	"slices"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// init_depends_seed.go — the init-runtime fetch SEED.

// InitDependsSeed is one init-runtime candy a scanned candy set implies must be fetched: the
// init build-vocabulary's own `depends_candy:` resolved into a fetch coordinate. Ref is the
// BARE ref (spec.BareCandyRef of the authored entry — no `@`, no `:version`), which is the form
// RemoteDownload.Refs carries; Version is the authored `:version` (empty when the vocabulary
// names a version-less ref, in which case the caller resolves the repo's default branch).
type InitDependsSeed struct {
	RepoPath string
	Version  string
	Ref      string
	Scope    string
}

// initDependsSeeds returns the init-runtime candies the init build-vocabulary implies for a
// given scanned candy set, so the fetch fix-point can materialize them.
//
// WHY THIS EXISTS. The init vocabulary declares, per init system, the candy that INSTALLS that
// init's runtime (`depends_candy:`). Declaring a `service:` in a candy is what SELECTS an init,
// but nothing ever FETCHED the selected init's runtime candy: InjectInitDependsCandy resolves
// `depends_candy` only against the project's already-scanned set, so a project that never
// happened to compose the init candy got `ai.opencharly.init="supervisord"` stamped onto an
// image whose supervisord candy was never built, and the build died inside podman on the
// render stage's COPY of the init candy's own template. The workaround — every service candy
// carrying `require: layer-supervisord`, and boxes listing it by hand — is target-blind and
// drags supervisord onto systemd/openrc venues; this seed is what replaces it.
//
// It is a SUPERSET by construction: it seeds an init's runtime candy whenever ANY scanned candy
// triggers that init, without knowing which box will resolve to it. That is deliberate and it
// is the only thing this layer CAN know — ScanCandyFromLocal has no config, so it cannot run
// ResolveInitSystem. The cost is a fetch that a particular project may not use; the benefit is
// that the fetch is never missing. Only `supervisord` declares a depends_candy today (systemd
// and openrc install with the OS), so in practice this seeds exactly one ref.
//
// A seed's ref is only seeded when the vocabulary names it as a REMOTE (`@`-prefixed) ref. A
// bare `depends_candy: supervisord` is left to the scan set exactly as before — a bare name
// has no repo to fetch from, and inventing one would be a guess.
func initDependsSeeds(scanned map[string]spec.ScannedCandy, initCfg *buildkit.InitConfig) []InitDependsSeed {
	if initCfg == nil {
		return nil
	}
	// The inits whose runtime candy is a fetchable remote ref, keyed by init name.
	remote := make(map[string]string, len(initCfg.Init))
	for initName, def := range initCfg.Init {
		if def == nil || def.DependsCandy == "" || !spec.IsRemoteCandyRefString(def.DependsCandy) {
			continue
		}
		remote[initName] = def.DependsCandy
	}
	if len(remote) == 0 {
		return nil
	}
	// Which of them does the scanned set actually trigger? Ranging `scanned` and `remote` is
	// unordered, so the result is sorted before it leaves: the download list is fed to a
	// network fix-point and its order must not vary run to run.
	seen := map[string]bool{}
	var out []InitDependsSeed
	for _, sc := range scanned {
		for initName, ref := range remote {
			if seen[initName] || !candyTriggersInit(sc, initCfg.Init[initName]) {
				continue
			}
			seen[initName] = true
			parsed := spec.ParseRemoteRef(ref)
			out = append(out, InitDependsSeed{
				RepoPath: parsed.RepoPath,
				Version:  parsed.Version,
				Ref:      spec.BareCandyRef(ref),
				// The scope label is the INIT, not a box: an init runtime is an INDEPENDENT
				// composition (spec.ScopeIsBox is false for it), so the arbiter's per-box
				// conflict rule (PickCandyVersion → scopeConflicts) never reports a version
				// difference between this seed and a project's own explicit pin of the same
				// candy. Reusing LayerScope keeps the scope grammar single-sourced (R3).
				Scope: spec.LayerScope("init:" + initName),
			})
		}
	}
	slices.SortFunc(out, func(a, b InitDependsSeed) int {
		if c := strings.Compare(a.RepoPath, b.RepoPath); c != 0 {
			return c
		}
		if c := strings.Compare(a.Version, b.Version); c != 0 {
			return c
		}
		return strings.Compare(a.Ref, b.Ref)
	})
	return out
}
