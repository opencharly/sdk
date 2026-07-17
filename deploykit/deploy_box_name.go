package deploykit

import "github.com/opencharly/sdk/spec"

// deploy_box_name.go — the PORTABLE half of the deploy-key → image-name resolver (K4). Charly
// core's own resolveDeployBoxName/resolveDeployKeyToBox (charly/deploy.go) stay unchanged — their
// project-level fallback reads LoadUnified directly, a cheap parse-only load core can always do.
// This portable twin serves a caller (a plugin, or any future consumer) that does NOT have a
// project loader but ALREADY holds a *spec.ResolvedProject (fetched once via HostBuild
// "resolved-project", e.g. for its own CandyModel/box-view needs) — its project-level fallback is
// a zero-cost map lookup against that already-fetched envelope's Deploy tree, never a fresh
// resolve. Both versions share the identical precedence: the per-host deploy-config overlay wins,
// then the project-level fallback, then the bare key.

// ResolveDeployBoxName is THE deploy-key→image-name resolver for a caller holding a
// *spec.ResolvedProject (rp may be nil — the project-level fallback is then skipped). Mirrors
// charly/deploy.go's resolveDeployBoxName precedence: the per-host deploy-config overlay
// (LoadDeployConfigForRead) wins, then rp.Deploy[key].Image, then the bare key.
func ResolveDeployBoxName(key, instance string, rp *spec.ResolvedProject) string {
	if img := resolveDeployKeyToBoxFromProject(key, instance, rp); img != "" {
		return img
	}
	return key
}

// resolveDeployKeyToBoxFromProject maps a deploy-key name to the `box:` field of its deploy entry,
// exactly as charly/deploy.go's resolveDeployKeyToBox does, except the project-level fallback reads
// an already-fetched rp.Deploy instead of calling LoadUnified.
func resolveDeployKeyToBoxFromProject(key, instance string, rp *spec.ResolvedProject) string {
	if key == "" {
		return ""
	}
	// User-side first.
	if dc := LoadDeployConfigForRead("resolveDeployKeyToBoxFromProject"); dc != nil {
		if entry, ok := dc.Bundle[DeployKey(key, instance)]; ok && entry.Image != "" {
			return entry.Image
		}
		if entry, ok := dc.Bundle[key]; ok && entry.Image != "" {
			return entry.Image
		}
	}
	// Project-level fallback — the already-fetched envelope, zero host round-trip.
	if rp != nil {
		if entry, ok := rp.Deploy[key]; ok && entry != nil && entry.Image != "" {
			return entry.Image
		}
	}
	return ""
}
