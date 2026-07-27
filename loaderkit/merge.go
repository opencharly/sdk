package loaderkit

import (
	"encoding/json"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// merge.go — the kind-blind document MERGE half of the unified-config loader
// (K1-proper, relocated from charly/unified.go). These are pure map/struct
// merges over an already-parsed UnifiedFile: no provider registry, no plugin
// round-trip, no charly-core helper. The host materialize (charly/materialize.go)
// replays the walk's documents in order — the root file first, then its flat
// imports — calling MergeUnified for each, so the root file's values are present
// before any import's fields are considered (root-wins). Boundary law clause M:
// a kind-blind mechanism that transforms an envelope, dispatched over data, never
// branching on a concrete kind — so it lives in the sdk loaderkit consumed by the
// loader plugin, not in charly/ core.

// MergeUnified merges src into dst such that dst's existing values WIN on
// conflict at the same leaf (root-wins). This means when materializeLoadedProject
// replays the walk's documents in order (the root file first, then its flat
// imports), the root file's values are already present before any import's
// fields are considered, so root wins.
//
// For included files: the same MergeUnified is called but dst already contains
// the root's values, so those fields stay untouched. src's fields that aren't
// present in dst get copied over. That's the desired semantics.
func MergeUnified(dst, src *UnifiedFile, srcDir string) {
	if src.Version != "" && dst.Version == "" {
		dst.Version = src.Version
	}
	// Root-wins: the root file (merged first) defines the project's repo
	// identity; a flat import declaring `repo:` never overrides it.
	if src.Repo != "" && dst.Repo == "" {
		dst.Repo = src.Repo
	}
	// Discover entries concatenate (not overwrite). Resolve relative
	// paths to absolute against srcDir so an included file's discover
	// roots remain anchored to the included file's directory rather
	// than to the eventual root file's directory. Without this, a
	// downstream workspace that `include:`-s an upstream charly.yml
	// would look for upstream's `candy/` inside the workspace tree.
	if len(src.Discover) > 0 {
		dst.Discover = append(dst.Discover, kit.AnchorScanSpecs(src.Discover, srcDir)...)
	}
	mergeRawTemplateMap(&dst.Box, src.Box)
	mergeRawTemplateMap(&dst.Candy, src.Candy)
	// PluginKinds carries every plugin-extracted kind — the build vocabulary
	// (distro/builder/init/resource), the Calamares target, sidecar/agent/module/
	// package-group, AND (K1 unit-1 follow-up) the 5 standalone-substrate-TEMPLATE kinds
	// vm/pod/k8s/local/android (formerly 5 separate mergeRawTemplateMap calls into dedicated
	// fields — now subsumed here too, since they fold into PluginKinds[disc][name] like every
	// other templated kind) — merged once here (root-wins, name-keyed override). The former
	// mergeDistroMap/mergeBuilderMap/mergeInitMap/mergeResourceMap/mergeTargetMap calls
	// are subsumed by this one generic merge.
	MergePluginKindsMap(&dst.PluginKinds, src.PluginKinds)
	mergeDeployMaps(&dst.Bundle, src.Bundle)
	if dst.Provides == nil && src.Provides != nil {
		dst.Provides = src.Provides
	}
	// Defaults: dst wins per-field if set.
	mergeBoxConfig(&dst.Defaults, &src.Defaults)
}

// mergeRawTemplateMap root-wins merges an OPAQUE substrate-template map (local /
// android after the Cutover I de-type): copy a name only when ABSENT in dst. One
// generic helper for both (R3) — the former typed mergeLocalMap/mergeAndroidMap.
func mergeRawTemplateMap(dst *map[string]json.RawMessage, src map[string]json.RawMessage) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]json.RawMessage)
	}
	for k, v := range src {
		if _, exists := (*dst)[k]; !exists {
			(*dst)[k] = v
		}
	}
}

// MergePluginKindsMap merges plugin-contributed kind entities (uf.PluginKinds:
// kind word → entity NAME → canonical entity JSON) across every merged
// document/file. Root-wins NAME-KEYED OVERRIDE, byte-identical in spirit to the
// build-vocab map merges (mergeDistroMap et al.): for each kind, an existing dst
// entry for a given name is PRESERVED and src fills only the names dst does not have.
// So a project's entity overrides an embedded/imported one of the same name (one
// entry, not two) — the property the agent + sidecar extractions rely on (a project's
// `sidecar: tailscale` overriding the binary-embedded one, merged in via
// applyEmbeddedDefaults). Without this,
// plugin-kind entities decoded into a per-document `sub` UnifiedFile are silently
// dropped at MergeUnified (every document flows through here).
func MergePluginKindsMap(dst *map[string]map[string]json.RawMessage, src map[string]map[string]json.RawMessage) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]map[string]json.RawMessage)
	}
	for kind, entities := range src {
		d := (*dst)[kind]
		if d == nil {
			d = make(map[string]json.RawMessage)
			(*dst)[kind] = d
		}
		for name, body := range entities {
			if _, exists := d[name]; !exists {
				d[name] = body
			}
		}
	}
}

// mergeDeployMaps merges src into dst, dst-wins on name collisions.
// Field-singular cutover: replaces the legacy mergeDeployments which
// took *DeploymentsSection wrappers. Provides now lives at UnifiedFile
// root and is merged separately by MergeUnified.
func mergeDeployMaps(dst *map[string]spec.BundleNode, src map[string]spec.BundleNode) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]spec.BundleNode)
	}
	for k, v := range src {
		if _, exists := (*dst)[k]; !exists {
			(*dst)[k] = v
		}
	}
}

// mergeBoxConfig preserves dst's already-set fields and fills only the
// zero-valued ones from src. Used for merging Defaults blocks from includes.
func mergeBoxConfig(dst, src *spec.BoxConfig) {
	if src == nil || dst == nil {
		return
	}
	if dst.Base == "" {
		dst.Base = src.Base
	}
	if dst.Tag == "" {
		dst.Tag = src.Tag
	}
	if dst.Registry == "" {
		dst.Registry = src.Registry
	}
	if len(dst.Platforms) == 0 {
		dst.Platforms = src.Platforms
	}
	if len(dst.Distro) == 0 {
		dst.Distro = src.Distro
	}
	if len(dst.Build) == 0 {
		dst.Build = src.Build
	}
	if len(dst.Candy) == 0 {
		dst.Candy = src.Candy
	}
	if dst.User == "" {
		dst.User = src.User
	}
	if dst.UID == nil {
		dst.UID = src.UID
	}
	if dst.GID == nil {
		dst.GID = src.GID
	}
	if dst.UserPolicy == "" {
		dst.UserPolicy = src.UserPolicy
	}
	if dst.Merge == nil {
		dst.Merge = src.Merge
	}
	if len(dst.Builder) == 0 {
		dst.Builder = src.Builder
	}
	if dst.Init == "" {
		dst.Init = src.Init
	}
	// Build-speed tunables (defaults: block) — carried through the same
	// per-field "dst wins if set" merge as the rest of BoxConfig.
	if dst.Jobs == nil {
		dst.Jobs = src.Jobs
	}
	if dst.PodmanJobs == nil {
		dst.PodmanJobs = src.PodmanJobs
	}
	if dst.PodmanJobsCap == nil {
		dst.PodmanJobsCap = src.PodmanJobsCap
	}
	if len(dst.ContextIgnore) == 0 {
		dst.ContextIgnore = src.ContextIgnore
	}
	if dst.Cache == "" {
		dst.Cache = src.Cache
	}
	if dst.KeepImages == nil {
		dst.KeepImages = src.KeepImages
	}
	if dst.KeepCheckRuns == nil {
		dst.KeepCheckRuns = src.KeepCheckRuns
	}
}
