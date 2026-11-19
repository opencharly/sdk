package deploykit

// read_labels.go — RE-EXPORT shim. The OCI-label → BoxMetadata extraction mechanism (the READ
// half of the OCI-label emission pair; WriteLabels in write_labels.go is the FORMAT-only emitter)
// lives in the spec/container fabric slice, github.com/opencharly/spec/container/box_metadata_coneb.go
// -- one canonical home (R3 single-source). This file re-exports ExtractMetadata + InspectLabels so
// every deploykit.ExtractMetadata / deploykit.InspectLabels call site (the candies -- plugin-check,
// plugin-deploy-pod, plugin-kube, plugin-pod) is unchanged. charly core does not read base-image
// labels; kit's own re-export (sdk/kit/box_metadata.go) names its single candy consumer. New
// consumers reference spec/container directly.
//
// Testability: override container.InspectLabels (the var container.ExtractMetadata reads) to stub
// label reads in tests of the decode logic; the deploykit.InspectLabels re-export var is a
// value-copy that does not affect the canonical body.

import "github.com/opencharly/spec/container"

// InspectLabels reads OCI labels from a local image via engine inspect. Package-level var for
// testability — re-exported from spec/container (the canonical home). Override container.InspectLabels
// to stub in tests.
var InspectLabels = container.InspectLabels

// ExtractMetadata reads OCI labels from a local image and returns parsed spec.BoxMetadata.
// Returns nil if the image has no ai.opencharly labels. Returns spec.ErrImageNotLocal wrapped
// with the image ref if the image is not in local storage.
var ExtractMetadata = container.ExtractMetadata
