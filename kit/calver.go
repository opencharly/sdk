package kit

import "github.com/opencharly/spec/calver"

// calver.go — re-export of the parsed CalVer schema-version type + the HEAD schema version /
// migration floor + comparator, RELOCATED to spec/calver (#55 value extraction).
// The parsed type is a pure value/transform over the version E-envelope, so it homes in spec; it
// is named calver.ParsedCalVer there (spec already binds CalVer=string, the CUE wire scalar). kit
// re-exports it as `type CalVer = calver.ParsedCalVer` + var forwarders so every existing
// kit.CalVer / kit.ParseCalVer / kit.LatestSchemaVersion / kit.SchemaFloor call site (charly core's
// migrate/version gate + plugin-box/clean/migrate) is unchanged. New consumers reference calver.*.
type CalVer = calver.ParsedCalVer

var (
	ParseCalVer         = calver.ParseCalVer
	MustCalVer          = calver.MustCalVer
	LatestSchemaVersion = calver.LatestSchemaCalVer
	SchemaFloor         = calver.SchemaFloorCalVer
)
