package spec

import "sort"

// distro_wire.go — the OpResolve envelope for the distro de-type (Cutover M, the long
// pole). candy/plugin-distro resolves an authored `distro:` build-vocabulary entity into
// a ResolvedDistro the kernel's build engine consumes without importing the concrete
// spec.Distro. The host keeps RenderTemplate + the cache-mount vocab (per the plan); the
// plugin owns the distro KNOWLEDGE (schema/typed shape/validation).

// ResolvedDistro is the resolve-to-envelope form of a `distro:` build-vocabulary entity.
// It mirrors spec.Distro's fields so every build-engine consumer reads it unchanged, and
// carries Raw for any verbatim pass-through.
type ResolvedDistro struct {
	Inherits        string             `yaml:"inherits,omitempty" json:"inherits,omitempty"`
	InheritPackages bool               `yaml:"inherit_packages,omitempty" json:"inherit_packages,omitempty"`
	Version         string             `yaml:"version,omitempty" json:"version,omitempty"`
	Bootstrap       Bootstrap          `yaml:"bootstrap,omitempty" json:"bootstrap,omitempty"`
	Workarounds     []string           `yaml:"workaround,omitempty" json:"workaround,omitempty"`
	Format          map[string]*Format `yaml:"format,omitempty" json:"format,omitempty"`
	BaseUser        *BaseUser          `yaml:"base_user,omitempty" json:"base_user,omitempty"`
	Pacstrap        *Pacstrap          `yaml:"pacstrap,omitempty" json:"pacstrap,omitempty"`
	Debootstrap     *Debootstrap       `yaml:"debootstrap,omitempty" json:"debootstrap,omitempty"`
	AlpineBootstrap *AlpineBootstrap   `yaml:"alpine_bootstrap,omitempty" json:"alpine_bootstrap,omitempty"`
	Bootloader      *Bootloader        `yaml:"bootloader,omitempty" json:"bootloader,omitempty"`
	Dnf             *Dnf               `yaml:"dnf,omitempty" json:"dnf,omitempty"`
	Raw             RawBody            `yaml:"-" json:"raw,omitempty"`
}

// PrimaryFormat returns the distro's primary (non-secondary) build format — the
// deterministic first non-secondary Format name (mirrors spec.Distro.PrimaryFormat).
func (d *ResolvedDistro) PrimaryFormat() string {
	if d == nil {
		return ""
	}
	names := make([]string, 0, len(d.Format))
	for name := range d.Format {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if fd := d.Format[name]; fd != nil && fd.Secondary {
			continue
		}
		return name
	}
	return ""
}

// LocalPkgFormat picks the format whose local_pkg block builds the charly toolchain:
// the caller's primary format, then PrimaryFormat, then any localpkg-capable format
// (mirrors spec.Distro.LocalPkgFormat).
func (d *ResolvedDistro) LocalPkgFormat(primaryFormat string) (string, *LocalPkg) {
	if d == nil {
		return "", nil
	}
	for _, fmtName := range []string{primaryFormat, d.PrimaryFormat()} {
		if fmtName == "" {
			continue
		}
		if fd := d.Format[fmtName]; fd != nil && fd.LocalPkg != nil {
			return fmtName, fd.LocalPkg
		}
	}
	names := make([]string, 0, len(d.Format))
	for name := range d.Format {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if fd := d.Format[name]; fd != nil && fd.LocalPkg != nil {
			return name, fd.LocalPkg
		}
	}
	return "", nil
}

// DistroResolveInput carries one opaque distro body to project.
type DistroResolveInput struct {
	Distro RawBody `json:"distro"`
}

// DistroResolveReply wraps the resolved distro.
type DistroResolveReply struct {
	Resolved *ResolvedDistro `json:"resolved,omitempty"`
}
