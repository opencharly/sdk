package buildkit

import "github.com/opencharly/spec/spec"

// init_config.go — the `init:` section of the build vocabulary + its detection/resolution logic. #55
// import-purity: the TYPE + its methods relocated DOWN to spec (spec/init_config.go, alongside the
// CUE-sourced DistroConfig/BuilderConfig vocabulary) so charly core reads it over its spec+proto-only
// import surface. This thin type-alias keeps buildkit's build-resolve callers (config_resolve.go /
// resolved_box.go) reading the ONE source unchanged (R3).

// InitConfig represents the `init:` section of the embedded vocabulary (charly/charly.yml).
type InitConfig = spec.InitConfig
