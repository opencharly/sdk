package buildkit

import (
	"slices"
	"sort"

	"github.com/opencharly/sdk/spec"
)

// Aliases onto the CUE-sourced spec types ResolvedBox references (DistroDef /
// DistroConfig / BuilderConfig are already bound in format_config.go, same
// package). These read unchanged in the moved code.
type (
	MergeConfig  = spec.MergeConfig
	ResolvedInit = spec.ResolvedInit
)

// BuilderMap is a map of build type → builder image name.
// Valid build types: pixi, npm, cargo, aur.
type BuilderMap map[string]string

// BuilderFor returns the builder image name for the given format, or "".
func (m BuilderMap) BuilderFor(format string) string {
	return m[format]
}

// HasBuilder returns true if a builder is configured for the given format.
func (m BuilderMap) HasBuilder(format string) bool {
	return m[format] != ""
}

// AllBuilder returns a deduplicated sorted list of builder image names.
func (m BuilderMap) AllBuilder() []string {
	seen := make(map[string]bool)
	var builders []string
	for _, b := range m {
		if b != "" && !seen[b] {
			seen[b] = true
			builders = append(builders, b)
		}
	}
	sort.Strings(builders)
	return builders
}

// AggregatedCandyCaps is the output of walking all candies in resolution
// order. It is populated onto ResolvedBox and consumed wherever code
// previously read BoxConfig.Bootc, BoxConfig.DataImage, or the
// init-system bootc parameter. The aggregation FUNCTION
// (AggregateCandyCapabilities, which walks the charly candy model) stays
// charly-side; only this pure result struct lives here.
type AggregatedCandyCaps struct {
	PreserveUser       bool
	NeedsRootAfterInit bool
	InitSystemHint     string
	DataOnly           bool
	OCILabels          map[string]string
	// Provided is the set of capability names declared by some candy in
	// the composition. Used by CheckRequiredCapabilities to validate
	// `requires_capabilities:` cross-candy requirements.
	Provided map[string]bool
}

// ResolvedBox represents a fully resolved box configuration
type ResolvedBox struct {
	Name    string
	Version string `json:"version,omitempty"` // authored per-entity CalVer (the box config `version:`); optional
	// EffectiveVersion is the content-derived identity emitted as the
	// ai.opencharly.version label: the dedicated Version if set, else the
	// highest candy version across the full chain (computeEffectiveVersions in
	// effective_version.go, run by the generator once the base chain +
	// auto-intermediates are materialized). Stable across builds when no candy
	// changed — this is what keeps a child's FROM <base> SHA from shifting.
	EffectiveVersion string `json:"effective_version,omitempty"`
	Status           string `json:"status,omitempty"`      // effective status (worst of box + candies)
	Info             string `json:"info,omitempty"`        // aggregated info from box + candies
	CheckLevel       string `json:"check_level,omitempty"` // acceptance-depth rung (none|build|noagent|agent), baked as ai.opencharly.check_level
	Base             string // Resolved base (external OCI ref or internal image name)
	// From mirrors BoxConfig.From after resolution. When non-empty
	// (e.g. "builder:pacstrap"), the generator emits FROM scratch +
	// ADD <staged-rootfs.tar.gz> instead of FROM <base>.
	From                  string
	BootstrapBuilderImage string
	Platforms             []string
	Tag                   string
	Registry              string
	Pkg                   string   // primary build format (first entry in BuildFormats) — for cache mounts, bootstrap
	Distro                []string // resolved distro tags: ["fedora:43", "fedora"]
	BuildFormats          []string // resolved build formats: ["rpm"] or ["pac", "aur"] — all installed in order
	Tags                  []string // union: ["all"] + Distro + BuildFormats — for task matching
	Candy                 []string

	// User configuration
	User string // username
	UID  int    // user ID
	GID  int    // group ID
	Home string // resolved home directory (detected or /home/<user>)
	// UserAdopted is true when the resolved user came from the distro's
	// base_user declaration (the embedded vocabulary's `distro.<name>.base_user`) rather
	// than being created by the bootstrap. Consulted by writeBootstrap to
	// skip the useradd step — the base image already ships this account.
	UserAdopted bool

	// Merge configuration
	Merge *MergeConfig // layer merge settings (nil means use CLI defaults)

	// Builder configuration (resolved: image -> base image -> defaults -> {})
	Builder BuilderMap // build type → builder image name
	// Builder capability declaration (image-specific, not inherited)
	BuilderCapabilities []string // what this builder image can build (from builds: field)

	// Auto-generated intermediate image
	Auto bool // true for auto-generated intermediate images

	// Schema v4: DNS / AcmeEmail / Tunnel / Engine removed from
	// ResolvedBox — they are deployment choices with no declaration
	// meaning. Consumers read them from BoxMetadata (post deploy-overlay)
	// instead of from the resolved image config.

	// Container network mode (e.g. "host", "none") — declaration of
	// required/recommended network mode. Deployment overrides via
	// MergeDeployOntoMetadata.
	Network string

	// Build config (resolved per-image via charly.yml import: + the binary-embedded build vocabulary)
	DistroConfig  *DistroConfig  `json:"-"` // distro section of the embedded vocabulary (charly/charly.yml)
	DistroDef     *DistroDef     `json:"-"` // resolved distro definition (cached)
	BuilderConfig *BuilderConfig `json:"-"` // builder section of the embedded vocabulary (charly/charly.yml)
	InitSystem    string         `json:"-"` // resolved init system name ("supervisord", "systemd", "")
	InitDef       *ResolvedInit  `json:"-"` // resolved init definition (cached)

	// Data image (scratch-based, data-only)
	DataImage bool // true = FROM scratch, no runtime, no init, no services

	// CandyCaps is the candy-derived capability surface aggregated from
	// this box's resolved candy composition (preserve_user, data_only,
	// init_system_hint, oci_labels, etc.). Populated by ResolveBox via
	// AggregateCandyCapabilities. The image-level flags (DataImage,
	// Bootc) remain as authored image-level fields alongside it.
	CandyCaps *AggregatedCandyCaps `json:"-"`

	// Derived fields
	IsExternalBase bool   // true if base is external OCI image, false if internal
	FullTag        string // registry/name:tag
}

// SupportsTag returns true if this image has the given tag.
// Tags include format (rpm, deb, pac), distro (fedora, arch),
// version (fedora:43), and the implicit "all".
func (img *ResolvedBox) SupportsTag(tag string) bool {
	return slices.Contains(img.Tags, tag)
}

// SupportsBuild returns true if this image has the given build format.
func (img *ResolvedBox) SupportsBuild(format string) bool {
	return slices.Contains(img.BuildFormats, format)
}
