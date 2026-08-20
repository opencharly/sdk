package buildkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// Tests for the (phase, venue) → template-string lookup on FormatDef / BuilderDef.
// Verifies (a) the phase: block is the single source of truth — the legacy
// top-level install_template was removed (R5) and its content migrated into
// phase.install.container, so there is NO fallback arm (a lookup returns ""
// when the requested cell is absent); (b) each (phase, venue) cell is read
// correctly; (c) nil receivers are safe.

func TestFormatDefPhaseTemplateCellLookup(t *testing.T) {
	f := &FormatDef{
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{
				Container: "RUN dnf install -y {{.Packages}}",
				Host:      "host-install",
			},
			Prepare: &spec.PhaseTemplates{
				Container: "RUN prepare-container",
				Host:      "prepare-host",
			},
			Cleanup: &spec.PhaseTemplates{
				Container: "RUN cleanup-container",
			},
		},
	}

	// (install, container) reads the phase cell.
	if got := FormatPhaseTemplate(f, spec.PhaseInstall, spec.VenueContainerBuilder); got != "RUN dnf install -y {{.Packages}}" {
		t.Errorf("(install, container) = %q, want the install.container cell", got)
	}
	// (install, host) reads the host cell.
	if got := FormatPhaseTemplate(f, spec.PhaseInstall, spec.VenueHostNative); got != "host-install" {
		t.Errorf("(install, host) = %q, want the install.host cell", got)
	}
	// Prepare + cleanup cells are read independently.
	if got := FormatPhaseTemplate(f, spec.PhasePrepare, spec.VenueContainerBuilder); got != "RUN prepare-container" {
		t.Errorf("(prepare, container) = %q", got)
	}
	if got := FormatPhaseTemplate(f, spec.PhasePrepare, spec.VenueHostNative); got != "prepare-host" {
		t.Errorf("(prepare, host) = %q", got)
	}
	// A phase with no Host cell returns "" for (phase, host) — no fallback arm.
	if got := FormatPhaseTemplate(f, spec.PhaseCleanup, spec.VenueHostNative); got != "" {
		t.Errorf("(cleanup, host) = %q, want empty (no host cell, no fallback)", got)
	}
}

func TestFormatDefPhaseTemplateNoFallback(t *testing.T) {
	// A FormatDef with no phases: block at all — every lookup must return "".
	f := &FormatDef{}
	for _, p := range []spec.Phase{spec.PhasePrepare, spec.PhaseInstall, spec.PhaseCleanup} {
		for _, v := range []spec.Venue{spec.VenueHostNative, spec.VenueContainerBuilder} {
			if got := FormatPhaseTemplate(f, p, v); got != "" {
				t.Errorf("no-phases lookup (%v, %v) = %q, want empty", p, v, got)
			}
		}
	}
}

func TestFormatDefPhaseTemplateNilSafe(t *testing.T) {
	var f *FormatDef
	if got := FormatPhaseTemplate(f, spec.PhaseInstall, spec.VenueContainerBuilder); got != "" {
		t.Errorf("nil FormatDef lookup = %q, want empty", got)
	}
}

func TestBuilderDefPhaseTemplateCellLookup(t *testing.T) {
	// An inline builder renders its container cell.
	inline := &BuilderDef{
		Inline: true,
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{
				Container: "RUN cargo install --path .",
			},
		},
	}
	if got := BuilderPhaseTemplate(inline, spec.PhaseInstall, spec.VenueContainerBuilder); got != "RUN cargo install --path ." {
		t.Errorf("inline builder (install, container) = %q, want the install.container cell", got)
	}
	// A builder without the cell returns "" — no legacy install_template fallback.
	multi := &BuilderDef{}
	if got := BuilderPhaseTemplate(multi, spec.PhaseInstall, spec.VenueContainerBuilder); got != "" {
		t.Errorf("no-phase builder fallback = %q, want empty", got)
	}
	if got := BuilderPhaseTemplate(multi, spec.PhaseInstall, spec.VenueHostNative); got != "" {
		t.Errorf("host-venue lookup = %q, want empty", got)
	}
}

func TestBuilderDefPathContributionsOptional(t *testing.T) {
	// Older build.yml entries don't have path_contributions — field is
	// optional and zero-value is nil/empty.
	b := &BuilderDef{}
	if len(b.PathContributions) != 0 {
		t.Errorf("default PathContributions len = %d, want 0", len(b.PathContributions))
	}
}
