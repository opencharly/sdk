package buildkit

import "github.com/opencharly/sdk/spec"

// phase_template.go — the (phase, venue) → template-string resolvers for the
// build-vocabulary FormatDef / BuilderDef. Pure over the CUE-sourced spec types
// (FormatDef = spec.Format, BuilderDef = spec.Builder, Phase/Venue = spec enums),
// so they live in sdk/buildkit beside the format/builder render surface (P8b);
// charly aliases them back via format_config.go.

// FormatPhaseTemplate looks up the template string for a (phase, venue)
// lookup, with documented fallback behavior: if the new phase: block
// lacks the requested cell, fall back to the legacy InstallTemplate for
// (PhaseInstall, container) only — the combination covered by the
// legacy field. All other lookups return "" when the new path is absent.
func FormatPhaseTemplate(f *FormatDef, phase spec.Phase, venue spec.Venue) string {
	if f == nil {
		return ""
	}
	if f.Phases != nil {
		var pt *spec.PhaseTemplates
		switch phase {
		case spec.PhasePrepare:
			pt = f.Phases.Prepare
		case spec.PhaseInstall:
			pt = f.Phases.Install
		case spec.PhaseCleanup:
			pt = f.Phases.Cleanup
		}
		if pt != nil {
			switch venue {
			case spec.VenueHostNative:
				if pt.Host != "" {
					return pt.Host
				}
			case spec.VenueContainerBuilder:
				if pt.Container != "" {
					return pt.Container
				}
			}
		}
	}
	// Legacy fallback: the old InstallTemplate only describes the
	// install-phase in container venue.
	if phase == spec.PhaseInstall && venue == spec.VenueContainerBuilder {
		return f.InstallTemplate
	}
	return ""
}

// BuilderPhaseTemplate is the BuilderDef analog of FormatPhaseTemplate.
// Same fallback rules apply: (PhaseInstall, container) falls back to the
// legacy inline InstallTemplate when Phases is absent.
func BuilderPhaseTemplate(b *BuilderDef, phase spec.Phase, venue spec.Venue) string {
	if b == nil {
		return ""
	}
	if b.Phases != nil {
		var pt *spec.PhaseTemplates
		switch phase {
		case spec.PhasePrepare:
			pt = b.Phases.Prepare
		case spec.PhaseInstall:
			pt = b.Phases.Install
		case spec.PhaseCleanup:
			pt = b.Phases.Cleanup
		}
		if pt != nil {
			switch venue {
			case spec.VenueHostNative:
				if pt.Host != "" {
					return pt.Host
				}
			case spec.VenueContainerBuilder:
				if pt.Container != "" {
					return pt.Container
				}
			}
		}
	}
	// Legacy fallback: an inline builder (cargo) uses InstallTemplate for the
	// container-shaped template. Multi-stage builders render via their plugin's
	// OpResolve (kit.BuilderResolve), NOT this fallback.
	if phase == spec.PhaseInstall && venue == spec.VenueContainerBuilder && b.Inline && b.InstallTemplate != "" {
		return b.InstallTemplate
	}
	return ""
}
