package buildkit

import "slices"

// DistroBuilderCandidate is one named entity's Distro tags + Builder map — the
// generic shape PickDistroBuilder consumes (R3: the shared abstraction behind
// the "distro-keyed builder default" lookup). Two callers adapt their OWN data
// source into this shape instead of each re-implementing the lookup:
//   - charly/config.go's Config.distroBuilderMap walks the UNRESOLVED project
//     config (c.allBoxNames() + c.BoxConfig(name)) — used by
//     resolveEffectiveBuilder, which runs BEFORE any buildkit.ResolvedBox
//     exists (per-image resolution, the remote-ref fetch walk), so no
//     resolved-box map is available yet.
//   - sdk/deploykit's ComputeIntermediates (intermediates_compute.go) walks the
//     ALREADY-RESOLVED boxes map it receives — the auto-intermediate synthesis
//     that runs strictly after ResolveAllBox.
type DistroBuilderCandidate struct {
	Name    string
	Distro  []string
	Builder BuilderMap
}

// PickDistroBuilder returns the builder map of the first candidate (in the
// given, caller-sorted order — determinism is the caller's responsibility, so
// the result is stable when more than one candidate shares a distro tag)
// whose Distro contains a tag, walking distroTags in priority order (most
// specific first, e.g. ["cachyos","arch"] or ["fedora:43","fedora"]). Only
// candidates with a non-empty Builder are considered.
func PickDistroBuilder(candidates []DistroBuilderCandidate, distroTags []string) BuilderMap {
	if len(distroTags) == 0 {
		return nil
	}
	for _, tag := range distroTags {
		for _, c := range candidates {
			if len(c.Builder) == 0 {
				continue
			}
			if slices.Contains(c.Distro, tag) {
				return c.Builder
			}
		}
	}
	return nil
}
