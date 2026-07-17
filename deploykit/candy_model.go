package deploykit

import "github.com/opencharly/sdk/spec"

// candy_model.go — CandyModel is now promoted to spec.CandyReader (W9: the mass-edit interface
// relocation). spec is the shared contract home an import-clean charly file can reach WITHOUT an
// sdk mechanism-kit import (mirrors the loader_seam.go DocParser/ProjectWalker precedent + the
// PackageSection/TagPkgConfig/RouteConfig/ApkPackageSpec/ServiceEntry/HooksConfig/SecurityConfig
// promotions already sitting in layer_model.go/steps.go/candy_field_aliases.go). This alias keeps
// every existing deploykit-internal + charly `deploykit.CandyModel` reference compiling unchanged;
// new call sites in an import-clean file should reach spec.CandyReader directly.
type CandyModel = spec.CandyReader
