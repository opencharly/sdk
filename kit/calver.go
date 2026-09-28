package kit

import "github.com/opencharly/spec/calver"

// calver.go — re-export of the parsed CalVer type + comparator, RELOCATED to spec/calver (#55
// value extraction). The parsed type is a pure value/transform over a CalVer string, so it homes
// in spec; it is named calver.ParsedCalVer there (spec already binds CalVer=string, a CUE wire
// scalar). kit re-exports it as `type CalVer = calver.ParsedCalVer` + var forwarders so existing
// kit.CalVer / kit.ParseCalVer call sites are unchanged.
//
// The former LatestSchemaVersion / SchemaFloor forwarders are GONE (the schema-versioning removal
// cutover): there is no schema HEAD/floor. CalVer remains a REAL identity — the binary's stamped
// build version and git release/candy tags — never an authored-file schema stamp.
type CalVer = calver.ParsedCalVer

var ParseCalVer = calver.ParseCalVer
