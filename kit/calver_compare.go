package kit

import "github.com/opencharly/spec/spec"

// calver_compare.go — re-export of the lenient dotted-string CalVer comparator, RELOCATED to
// spec/spec/calver_parse.go (#55 value extraction). kit re-exports it so every existing
// kit.CompareCalVer call site (charly image-tag logic + sdk/deploykit render engine) is unchanged.
var CompareCalVer = spec.CompareCalVer
