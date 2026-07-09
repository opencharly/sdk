package deploykit

// layer_model.go — the pure candy/layer package-config field types that the runtime
// Candy graph carries. Moved to sdk/deploykit in P4 (with Candy + the compiler); the
// charly loader populates them, the compiler reads them.

// PackageSection represents a generic format-specific package config in the candy manifest.
// All fields from the YAML section are available in Raw for template rendering.
type PackageSection struct {
	FormatName string         // "rpm", "deb", "pac", "aur", etc.
	Packages   []string       // extracted from Raw["package"] for quick access
	Raw        map[string]any // all fields from YAML, passed to templates
}

// TagPkgConfig is a distro/version-specific package config (e.g. `debian:13:`,
// `ubuntu:24.04:`, `fedora:43:`). Packages are installed using the primary
// format's tool (dnf, apt, pacman). Raw captures the full YAML so that tag
// sections can carry `repos:`, `options:`, `keys:` — the same schema as the
// generic format section — for version-specific upstream repo configurations.
type TagPkgConfig struct {
	Package []string       `yaml:"package,omitempty" json:"package,omitempty"`
	Raw     map[string]any `yaml:"-"`
}

// RouteConfig represents a route file declaration.
type RouteConfig struct {
	Host string
	Port string
}
