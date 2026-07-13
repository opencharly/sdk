package deploykit

import "github.com/opencharly/sdk/spec"

// layer_model.go — the pure candy/layer package-config field types the runtime Candy graph
// carries. CUE-SOURCED in spec now (sdk/schema/candymodel.cue, the S-CM enabler) so #CandyModel
// can compose them; these ALIAS onto spec (SDD: wire types are CUE-sourced, one source). The
// charly loader populates them, the compiler + validate read them.

// PackageSection — a generic format-specific package section (rpm/deb/pac/aur). Raw carries the
// full YAML map for template rendering.
type PackageSection = spec.PackageSection

// TagPkgConfig — a distro/version-specific package section (debian:13, ubuntu:24.04, fedora:43…).
// Raw captures the full YAML so tag sections carry repos:/options:/keys:.
type TagPkgConfig = spec.TagPkgConfig

// RouteConfig — a resolved route file declaration (host + port-as-string).
type RouteConfig = spec.RouteConfig
