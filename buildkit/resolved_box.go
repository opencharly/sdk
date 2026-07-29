package buildkit

import (
	"github.com/opencharly/spec/spec"
)

// Aliases onto the CUE-sourced spec types ResolvedBox references (DistroDef /
// DistroConfig / BuilderConfig are already bound in format_config.go, same
// package). These read unchanged in the moved code.
type (
	MergeConfig         = spec.MergeConfig
	ResolvedInit        = spec.ResolvedInit
	BuilderMap          = spec.BuilderMap
	AggregatedCandyCaps = spec.AggregatedCandyCaps
)

// ResolvedBox represents a fully resolved box configuration. #55 step3 unit 3a: the
// wire-clean value fields (Name/Version/Distro/Tags/User/…) moved to spec.ResolvedBox
// (spec/spec/resolved_box.go) — the shared contract type charly core's render-seam floor
// + the "resolved-project" envelope projector + candy/plugin-build all read/construct.
// This type is now an EMBEDDING WRAPPER adding back ONLY the genuinely
// buildkit-owned host-render-cache pointer FIELDS (the kit.CheckResult/DeadlineExceeded
// pattern — see /charly-internals:go "Generation coverage"). Their TYPES —
// DistroConfig/DistroDef/BuilderConfig — are the parsed distro:/builder: build
// VOCABULARY, all CUE-sourced spec types (spec.DistroConfig / spec.ResolvedDistro /
// spec.BuilderConfig), referenced here through the thin buildkit aliases in
// format_config.go (#55 step 3-III relocated the container types + their
// vocabulary-resolution methods to spec). Every promoted spec.ResolvedBox field (Name, Tags, UID, Merge,
// Builder, CandyCaps, BakedMetadata, …) reads/writes unchanged via selector
// (img.Name, img.UID = x, …); only COMPOSITE-LITERAL construction sites needed
// restructuring to nest the wire-clean fields under the embedded field name.
type ResolvedBox struct {
	spec.ResolvedBox

	// Build config (resolved per-image via charly.yml import: + the binary-embedded build vocabulary)
	DistroConfig  *DistroConfig  `json:"-"` // distro section of the embedded vocabulary (charly/charly.yml)
	DistroDef     *DistroDef     `json:"-"` // resolved distro definition (cached)
	BuilderConfig *BuilderConfig `json:"-"` // builder section of the embedded vocabulary (charly/charly.yml)
	InitSystem    string         `json:"-"` // resolved init system name ("supervisord", "systemd", "")
	InitDef       *ResolvedInit  `json:"-"` // resolved init definition (cached)

	// CandyCaps is the candy-derived capability surface aggregated from this box's resolved
	// candy composition (preserve_user, data_only, init_system_hint, oci_labels, etc.).
	// Populated by ResolveBox via AggregateCandyCapabilities. Excluded from #ResolvedBox's CUE
	// def (gengotypes has no json:"-" construct — the spike-proven exception, see
	// spec/schema/resolvedbox.cue) — hand-kept here alongside the other host-render caches.
	CandyCaps *AggregatedCandyCaps `json:"-"`

	// --- build-RENDER inputs (#67, the build_resolve RENDER-leg death) ---
	// Host-RESOLVED render inputs the deploykit render reads as a pure ORCHESTRATOR. All
	// json:"-": a render compute cache, never the box's own inspect wire identity (the wire
	// copy rides ResolvedBoxView, filled by the projector). Excluded from #ResolvedBox for the
	// same json:"-" reason as CandyCaps, above.
	BakedMetadata    *spec.BakedLabelSet      `json:"-"` // the fully-baked OCI-label wire set the render's WriteLabels FORMATS
	RenderCandyOrder []string                 `json:"-"` // the per-box cache-optimal candy order (globalOrderForBox)
	ActiveInits      map[string]*ResolvedInit `json:"-"` // the active init systems for this composition (ActiveInit)
}
