package packagekit

// variants.go — resolve a variant's plugin list + package name from the packaging
// section. The format's `default_variant` (or the variant named "default" when
// unset) packages as the plain `charly` package; every other named variant packages
// as `charly-<variant>`.

import (
	"fmt"
	"sort"

	"github.com/opencharly/spec/spec"
)

// ResolveVariant returns the package name + the variant's plugin list for a
// (format, variant) pair. variant == "" selects the format's default variant.
func ResolveVariant(pkg *spec.Packaging, format, variant string) (name string, plugins []string, err error) {
	if pkg == nil {
		return "", nil, fmt.Errorf("no packaging section")
	}
	if pkg.Name == "" {
		return "", nil, fmt.Errorf("packaging.name is empty")
	}
	f := pkg.Formats[format]
	if f == nil {
		return "", nil, fmt.Errorf("packaging.formats has no %q entry", format)
	}
	if variant == "" {
		variant = f.DefaultVariant
	}
	if variant == "" {
		variant = "default"
	}
	v := pkg.Variants[variant]
	if v == nil {
		return "", nil, fmt.Errorf("packaging.variants has no %q variant", variant)
	}
	if variant == f.DefaultVariant || (f.DefaultVariant == "" && variant == "default") {
		return pkg.Name, v.Plugins, nil
	}
	return pkg.Name + "-" + variant, v.Plugins, nil
}

// VariantNames returns the sorted variant names for a format (the default variant
// first). Used by the distro workflows to loop over variants.
func VariantNames(pkg *spec.Packaging, format string) []string {
	f := pkg.Formats[format]
	if f == nil {
		return nil
	}
	def := f.DefaultVariant
	if def == "" {
		def = "default"
	}
	names := make([]string, 0, len(pkg.Variants))
	for name := range pkg.Variants {
		if name != def {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return append([]string{def}, names...)
}
