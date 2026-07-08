package buildkit

import (
	"sort"

	"github.com/opencharly/sdk/spec"
)

// Local aliases onto the CUE-sourced spec resolved-config types, so the moved
// vocabulary-resolution code below reads unchanged. These are the SAME types
// charly aliases package-main-side (vmshared_aliases.go); buildkit binds them
// here so the resolver is importable without charly core.
type (
	DistroDef  = spec.ResolvedDistro
	FormatDef  = spec.Format
	BuilderDef = spec.Builder
)

// --- Distro Config ---

// DistroConfig represents the `distro:` section of the embedded vocabulary (charly/charly.yml).
// Each distro defines bootstrap behavior AND package format definitions.
type DistroConfig struct {
	Distro map[string]*DistroDef `yaml:"distro" json:"distro"`
}

// ResolveDistro finds the distro definition matching the image's distro tags.
// Walks tags in order, strips :version suffix to match base distro name.
// Follows inherits: chains with cycle detection, inheriting formats from parent.
func (dc *DistroConfig) ResolveDistro(distroTags []string) *DistroDef {
	if dc == nil {
		return nil
	}
	for _, tag := range distroTags {
		// Try exact match first (e.g., "fedora:43")
		if def, ok := dc.Distro[tag]; ok {
			return dc.ResolveInherits(def, 10)
		}
		// Try base name (e.g., "fedora" from "fedora:43")
		base := tag
		if idx := indexOf(tag, ':'); idx >= 0 {
			base = tag[:idx]
		}
		if def, ok := dc.Distro[base]; ok {
			return dc.ResolveInherits(def, 10)
		}
	}
	return nil
}

func (dc *DistroConfig) ResolveInherits(def *DistroDef, maxDepth int) *DistroDef {
	if def.Inherits == "" || maxDepth <= 0 {
		return def
	}
	parent, ok := dc.Distro[def.Inherits]
	if !ok {
		return def
	}
	resolved := dc.ResolveInherits(parent, maxDepth-1)

	baseUser := def.BaseUser
	if baseUser == nil {
		baseUser = resolved.BaseUser
	}
	pacstrap := def.Pacstrap
	if pacstrap == nil {
		pacstrap = resolved.Pacstrap
	}
	debootstrap := def.Debootstrap
	if debootstrap == nil {
		debootstrap = resolved.Debootstrap
	}
	alpineBootstrap := def.AlpineBootstrap
	if alpineBootstrap == nil {
		alpineBootstrap = resolved.AlpineBootstrap
	}
	bootloader := def.Bootloader
	if bootloader == nil {
		bootloader = resolved.Bootloader
	}
	dnf := def.Dnf
	if dnf == nil {
		dnf = resolved.Dnf
	}
	version := def.Version
	if version == "" {
		version = resolved.Version
	}

	if def.Bootstrap.InstallCmd != "" {
		// Child has its own bootstrap. Merge inherited optional sub-blocks onto it.
		formats := def.Format
		if len(formats) == 0 {
			formats = resolved.Format
		}
		merged := &DistroDef{
			Inherits:        def.Inherits,
			Version:         version,
			Bootstrap:       def.Bootstrap,
			Workarounds:     def.Workarounds,
			Format:          formats,
			BaseUser:        baseUser,
			Pacstrap:        pacstrap,
			Debootstrap:     debootstrap,
			AlpineBootstrap: alpineBootstrap,
			Bootloader:      bootloader,
			Dnf:             dnf,
		}
		return merged
	}
	// Child has no bootstrap — inherit parent's bootstrap + workarounds,
	// overlay child's formats / baseuser / new sub-blocks.
	formats := resolved.Format
	if len(def.Format) > 0 {
		formats = def.Format
	}
	merged := &DistroDef{
		Inherits:        def.Inherits,
		Version:         version,
		Bootstrap:       resolved.Bootstrap,
		Workarounds:     resolved.Workarounds,
		Format:          formats,
		BaseUser:        baseUser,
		Pacstrap:        pacstrap,
		Debootstrap:     debootstrap,
		AlpineBootstrap: alpineBootstrap,
		Bootloader:      bootloader,
		Dnf:             dnf,
	}
	return merged
}

// AllFormatNames returns a sorted, deduplicated list of all format names across all distros.
func (dc *DistroConfig) AllFormatNames() []string {
	if dc == nil {
		return nil
	}
	seen := make(map[string]bool)
	for _, distro := range dc.Distro {
		resolved := dc.ResolveInherits(distro, 10)
		for name := range resolved.Format {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DistroTagChain builds the most-specific-first distro tag chain used by the
// cascade resolver: [<distro>:<version>, <distro>] when a canonical version is
// known (e.g. ["ubuntu:24.04", "ubuntu"]), or just [<distro>] for a rolling
// distro with no version (arch/cachyos).
func DistroTagChain(distro, version string) []string {
	if distro == "" {
		return nil
	}
	if version == "" {
		return []string{distro}
	}
	return []string{distro + ":" + version, distro}
}

// bareDistroName strips an optional ":<version>" suffix from a distro tag,
// returning the base distro name (`debian:13` → `debian`, `cachyos` → `cachyos`).
func bareDistroName(tag string) string {
	if i := indexOf(tag, ':'); i >= 0 {
		return tag[:i]
	}
	return tag
}

// ExpandPackageInheritance appends, AFTER the authored distro tags, every
// `inherits:` ancestor of a tag whose distro def opts into package inheritance
// via `inherit_packages: true` — most-specific authored tags kept first,
// ancestors appended as the least-specific levels.
func (dc *DistroConfig) ExpandPackageInheritance(tags []string) []string {
	if dc == nil || len(tags) == 0 {
		return tags
	}
	out := append([]string(nil), tags...)
	seen := map[string]bool{}
	for _, t := range tags {
		seen[bareDistroName(t)] = true
	}
	for _, t := range tags {
		name := bareDistroName(t)
		for {
			def := dc.Distro[name]
			if def == nil || !def.InheritPackages || def.Inherits == "" {
				break
			}
			parent := def.Inherits
			if !seen[parent] {
				out = append(out, parent)
				seen[parent] = true
			}
			name = parent
		}
	}
	return out
}

// FindFormat returns the FormatDef for a format name (rpm/deb/pac/aur),
// resolving distro `inherits:` chains. The first distro that defines the format
// wins. Returns nil when no distro defines it.
func (dc *DistroConfig) FindFormat(name string) *FormatDef {
	if dc == nil {
		return nil
	}
	for _, distro := range dc.Distro {
		resolved := dc.ResolveInherits(distro, 10)
		if fd := resolved.Format[name]; fd != nil {
			return fd
		}
	}
	return nil
}

// WrapDistroDef presents one already-resolved DistroDef as a DistroConfig so the
// format-keyed FindFormat resolver returns that def's FormatDef. Returns nil for a
// nil def.
func WrapDistroDef(def *DistroDef) *DistroConfig {
	if def == nil {
		return nil
	}
	return &DistroConfig{Distro: map[string]*DistroDef{"resolved": def}}
}

// ValidFormat returns true if any distro defines this format name.
func (dc *DistroConfig) ValidFormat(name string) bool {
	if dc == nil {
		return false
	}
	for _, distro := range dc.Distro {
		resolved := dc.ResolveInherits(distro, 10)
		if _, ok := resolved.Format[name]; ok {
			return true
		}
	}
	return false
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// --- Builder Config ---

// BuilderConfig represents the `builder:` section of the embedded vocabulary (charly/charly.yml).
type BuilderConfig struct {
	Builder map[string]*BuilderDef `yaml:"builder" json:"builder"`
}

// ValidBuilderType returns true if the given name is a defined builder.
func (bc *BuilderConfig) ValidBuilderType(name string) bool {
	if bc == nil {
		return false
	}
	_, ok := bc.Builder[name]
	return ok
}

// BuilderNames returns sorted list of defined builder names.
func (bc *BuilderConfig) BuilderNames() []string {
	if bc == nil {
		return nil
	}
	names := make([]string, 0, len(bc.Builder))
	for name := range bc.Builder {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
