package kit

import "github.com/opencharly/spec/spec"

// remote_ref.go — re-export of the remote-ref parsing helpers, RELOCATED to spec (#55 value
// extraction). The ref-parsing VOCAB (ParsedRef/IsRemoteImageRef/ParseRemoteRef/SplitRepoAndSubPath)
// already lived in spec/spec/ref_parse.go; kit's former self-contained copy was a duplicate (R3),
// now collapsed. StripURLScheme/ResolveBoxName rejoined it in spec/spec/box_name.go. kit re-exports
// them here so every existing kit.ParseRemoteRef / kit.ResolveBoxName / … call site (plugins + sdk)
// is untouched. New consumers should reference spec.* directly.
type ParsedRef = spec.ParsedRef

var (
	StripURLScheme   = spec.StripURLScheme
	IsRemoteImageRef = spec.IsRemoteImageRef
	ParseRemoteRef   = spec.ParseRemoteRef
	ResolveBoxName   = spec.ResolveBoxName
)
