package kit

// box_metadata.go — RE-EXPORT shim. The OCI-label → BoxMetadata extraction mechanism was
// RELOCATED to the spec/container fabric slice (#55 coneB build-render cone, Class A —
// github.com/opencharly/spec/container/box_metadata_coneb.go). ExtractMetadata + the InspectLabels
// testability var now live there; this file re-exports each so every existing kit.ExtractMetadata /
// kit.InspectLabels call site (charly core's build_overlay.go + the candies) is unchanged.
// New consumers reference spec/container directly. The R3 duplicate in sdk/deploykit/read_labels.go
// is likewise collapsed to a re-export of the same canonical home (one source, R3).
//
// Testability: override container.InspectLabels (the var container.ExtractMetadata reads) to stub
// label reads in tests of the decode logic; the kit.InspectLabels re-export var is a value-copy that
// no longer affects the relocated body.

import "github.com/opencharly/spec/container"

// InspectLabels reads OCI labels from a local image via engine inspect. Package-level var for
// testability — re-exported from spec/container (the canonical home). Override container.InspectLabels
// to stub in tests.
var InspectLabels = container.InspectLabels

// ExtractMetadata reads OCI labels from a local image and returns parsed spec.BoxMetadata.
// Returns nil if the image has no ai.opencharly labels. Returns spec.ErrImageNotLocal wrapped
// with the image ref if the image is not in local storage.
var ExtractMetadata = container.ExtractMetadata
