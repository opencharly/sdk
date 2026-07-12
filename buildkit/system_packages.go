package buildkit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// system_packages.go — the PURE build-context (container-venue) render of a
// SystemPackages install step's Containerfile fragment. Relocated from charly
// core's stepEmitSystemPackages (P8b): given the step's format + phase + raw
// install context and the box-resolved DistroConfig, it resolves the FormatDef,
// picks the phase.install.container template, and renders it — all over sdk types
// (FormatDef = spec.Format, Phase = spec enum, DistroConfig/NewInstallContext/
// RenderTemplate already buildkit). The core stepEmitSystemPackages keeps the
// wire-view → concrete-step reconstruction (deploykit-coupled) and threads the
// pure scalars in.

// RenderSystemPackagesFragment renders the container-venue Containerfile fragment
// for a SystemPackages step. A nil FormatDef (no distro defines the format) is a
// LOUD error; an empty template for the phase/venue is legitimately nothing to
// emit (returns ""). Byte-identical to the former in-core render.
func RenderSystemPackagesFragment(format string, phase spec.Phase, rawInstallContext map[string]any, distroCfg *DistroConfig) (string, error) {
	formatDef := distroCfg.FindFormat(format)
	if formatDef == nil {
		return "", fmt.Errorf("no distro definition for format %q", format)
	}
	template := FormatPhaseTemplate(formatDef, phase, spec.VenueContainerBuilder)
	if template == "" {
		// No template for this phase/venue is not an error — some phases simply have
		// nothing to emit in the container (e.g. cleanup phases whose host: blocks only
		// record state for teardown).
		return "", nil
	}
	ctx := NewInstallContext(rawInstallContext, formatDef.CacheMount)
	rendered, err := RenderTemplate(format+"-install", template, ctx)
	if err != nil {
		return "", fmt.Errorf("rendering %s install template: %w", format, err)
	}
	return rendered, nil
}
