package loaderkit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/buildkit"
)

// resolve_opts.go — the loader-config OPTIONS + the loader VALIDATION accumulator, relocated from
// charly core (config.go's ResolveOpts + validate.go's ValidationError) in the #118 Cluster-A
// loader-projection keystone. Both are loader types (not charly-core capability), so they live in
// loaderkit where charly core AND the loader-consuming plugins share ONE definition (R3, ZERO
// charly-core alias). ResolveOpts is DISTINCT from buildkit.ResolveOpts (the build-resolve options):
// this is the SCAN/LOAD options the candy scan + project validation consume; the moved buildkit
// resolvers never read ExtraCandyRefs/InitCfg/RequestedBoxes.

// ResolveOpts carries the scan/load options threaded through the candy scan + project resolution.
type ResolveOpts struct {
	IncludeDisabled      bool            // skip the `enabled: false` check
	IncludeDisabledNames map[string]bool // when non-empty, scope IncludeDisabled to these names only
	// RequestedBoxes are the explicit build targets (`charly box build <name>`). A qualified name
	// here (e.g. `charly.arch-builder`) is pulled into the resolved set even when it isn't reachable
	// as a base/builder of a root image — so a namespaced image can be an on-demand build target, not
	// only a transitive base. Bare names are ignored here (they resolve through the root loop).
	RequestedBoxes []string
	// ExtraCandyRefs are candy refs to collect IN ADDITION to the image/builder/kind:local-template
	// closure — specifically a DEPLOY's `add_candy:` candies. The image-closure walk (collectBox)
	// never reaches them, so a bed that add_candy's a host-side PLUGIN candy must pass its add_candy
	// refs here, or the plugin never enters the candy scan and loadProjectPlugins can't build/connect
	// it. NEVER read by the moved buildkit resolvers — consumed solely by the candy scan.
	ExtraCandyRefs []string
	// InitCfg is the project init: vocabulary (W9), threaded through so the candy scan can run the
	// cross-candy init-system host-completion pass (PopulateCandyInitSystem) BEFORE wrapping each
	// candy into the FINAL spec.CandyReader. A caller that leaves this nil skips the pass (correct
	// only for a caller with no init-aware consumer downstream). NEVER read by the buildkit resolvers.
	InitCfg *buildkit.InitConfig
	// DistroCfg / BuilderCfg are the project's build vocabulary (distro:/builder:), threaded through
	// so a resolve does not re-run the project load on every call (a caller with the triple, or a
	// multi-box loop, sets it once and skips the redundant reload; nil is byte-identical fallback).
	DistroCfg  *buildkit.DistroConfig
	BuilderCfg *buildkit.BuilderConfig
}

// ShouldIncludeDisabled reports whether name's disabled gate should be bypassed under opts.
// Centralizes the IncludeDisabled + IncludeDisabledNames interaction so call sites stay simple.
func (opts ResolveOpts) ShouldIncludeDisabled(name string) bool {
	if !opts.IncludeDisabled {
		return false
	}
	if len(opts.IncludeDisabledNames) == 0 {
		return true
	}
	return opts.IncludeDisabledNames[name]
}

// ValidationError accumulates project/candy validation error messages (the loader validation
// accumulator, distinct from the richer spec.Diagnostics — a plain []string collector).
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("validation error: %s", e.Errors[0])
	}
	return fmt.Sprintf("%d validation errors:\n\n  %s", len(e.Errors), strings.Join(e.Errors, "\n  "))
}

// Add adds an error to the collection.
func (e *ValidationError) Add(format string, args ...any) {
	e.Errors = append(e.Errors, fmt.Sprintf(format, args...))
}

// HasErrors returns true if there are any errors.
func (e *ValidationError) HasErrors() bool {
	return len(e.Errors) > 0
}
