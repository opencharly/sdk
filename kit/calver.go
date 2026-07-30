package kit

import "github.com/opencharly/spec/spec"

// calver.go — re-export of the parsed CalVer schema-version type + the HEAD schema version /
// migration floor + comparator, RELOCATED to spec/spec/calver_parse.go (#55 value extraction).
// The parsed type is a pure value/transform over the version E-envelope, so it homes in spec; it
// is named spec.ParsedCalVer there (spec already binds CalVer=string, the CUE wire scalar). kit
// re-exports it as `type CalVer = spec.ParsedCalVer` + var forwarders so every existing
// kit.CalVer / kit.ParseCalVer / kit.LatestSchemaVersion / kit.SchemaFloor call site (charly core's
// migrate/version gate + plugin-box/clean/migrate) is unchanged. New consumers reference spec.*.
type CalVer = spec.ParsedCalVer

var (
	ParseCalVer         = spec.ParseCalVer
	MustCalVer          = spec.MustCalVer
	LatestSchemaVersion = spec.LatestSchemaCalVer
	SchemaFloor         = spec.SchemaFloorCalVer
)
